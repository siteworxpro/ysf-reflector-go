package reflector

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/siteworxpro/ysf-reflector-go/internal/config"
	"github.com/siteworxpro/ysf-reflector-go/internal/web"
)

// TransmissionEntry records a completed transmission.
type TransmissionEntry struct {
	Callsign  string
	StartedAt time.Time
	EndedAt   time.Time
	Duration  time.Duration
}

// ActiveTransmitter describes the node currently on air.
type ActiveTransmitter struct {
	Callsign  string
	StartedAt time.Time
}

const txLogMax = 50 // number of entries to retain in the ring buffer

// Notifier is satisfied by *web.Server and allows the reflector to push state
// updates to connected WebSocket clients without creating an import cycle.
type Notifier interface {
	Notify()
}

// Reflector is a YSF (Yaesu System Fusion) UDP reflector.
// It listens for YSFP keepalive polls and YSFD voice/data frames, maintaining
// a list of active nodes and relaying incoming frames to all other nodes.
type Reflector struct {
	cfg      *config.Config
	conn     *net.UDPConn
	clients  *clientStore
	callsign []byte // 10-byte padded
	notifier Notifier

	// Transmission watchdog — tracks the currently active transmitter.
	watchdogMu      sync.Mutex
	watchdogTimer   *time.Timer
	watchdogCurrent string    // callsign currently on air, empty when idle
	watchdogStarted time.Time // when the current transmission began

	// Transmission log — ring buffer of completed transmissions.
	txMu  sync.RWMutex
	txLog []TransmissionEntry

	// Parrot mode — frames are buffered during TX and replayed after it ends.
	parrotMu  sync.Mutex
	parrotBuf [][]byte
}

// Clients returns a snapshot of all currently connected clients.
// It satisfies web.ClientProvider without creating an import cycle.
func (r *Reflector) Clients() []web.ClientInfo {
	raw := r.clients.snapshot()
	out := make([]web.ClientInfo, 0, len(raw))
	for _, c := range raw {
		out = append(out, web.ClientInfo{
			Callsign: c.callsign,
			Addr:     c.addr.String(),
			LastSeen: c.lastSeen,
		})
	}
	return out
}

// ActiveTransmitter returns the node currently on air, or nil if the
// reflector is idle.
func (r *Reflector) ActiveTransmitter() *web.ActiveTransmitterInfo {
	r.watchdogMu.Lock()
	defer r.watchdogMu.Unlock()
	if r.watchdogCurrent == "" {
		return nil
	}
	return &web.ActiveTransmitterInfo{
		Callsign:  r.watchdogCurrent,
		StartedAt: r.watchdogStarted,
	}
}

// TransmissionLog returns a copy of the completed-transmission ring buffer,
// most-recent first.
func (r *Reflector) TransmissionLog() []web.TransmissionEntryInfo {
	r.txMu.RLock()
	defer r.txMu.RUnlock()
	out := make([]web.TransmissionEntryInfo, len(r.txLog))
	for i, e := range r.txLog {
		out[i] = web.TransmissionEntryInfo(e)
	}
	return out
}

// New creates a Reflector from the supplied configuration.
func New(cfg *config.Config) *Reflector {
	return &Reflector{
		cfg:      cfg,
		clients:  newClientStore(),
		callsign: cfg.PaddedCallsign(),
	}
}

// Run starts the reflector, blocking until an unrecoverable error occurs.
func (r *Reflector) Run() error {
	addr := &net.UDPAddr{Port: r.cfg.Port}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("listen UDP :%d: %w", r.cfg.Port, err)
	}
	r.conn = conn
	defer func() { _ = conn.Close() }()

	log.Printf("YSF reflector %s listening on UDP port %d (client timeout %ds)",
		r.cfg.Callsign, r.cfg.Port, r.cfg.Timeout)

	webSrv, err := web.New(r.cfg, r)
	if err != nil {
		return fmt.Errorf("init web server: %w", err)
	}
	r.notifier = webSrv
	go func() {
		if err := webSrv.ListenAndServe(); err != nil {
			log.Printf("web server error: %v", err)
		}
	}()

	go r.evictLoop()
	go r.serverPollLoop()
	go r.statusDumpLoop()

	buf := make([]byte, 512)
	for {
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			return fmt.Errorf("read UDP: %w", err)
		}
		r.handle(buf[:n], remote)
	}
}

// handle dispatches an incoming UDP packet by its 4-byte magic prefix.
func (r *Reflector) handle(pkt []byte, src *net.UDPAddr) {
	if len(pkt) < 4 {
		return
	}

	switch string(pkt[0:4]) {
	case magicPoll:
		r.handlePoll(pkt, src)
	case magicData:
		r.handleData(pkt, src)
	case magicUnlink:
		r.handleUnlink(pkt, src)
	case magicStatus:
		r.handleStatus(src)
	case magicOption, magicInfo:
		// Silently ignored per reference implementation.
		if r.cfg.Debug {
			log.Printf("ignoring unsupported packet type %q from %s", pkt[0:4], src)
		}
	default:
		if r.cfg.Debug {
			log.Printf("unknown packet type %q from %s", pkt[0:4], src)
		}
	}
}

// handlePoll processes a YSFP keepalive from a node.
// Registers/refreshes the node and echoes the reflector's own callsign back.
func (r *Reflector) handlePoll(pkt []byte, src *net.UDPAddr) {
	if len(pkt) < PollSize {
		return
	}
	callsign := parsePollCallsign(pkt)

	isNew := r.clients.upsert(src, callsign)
	if isNew {
		log.Printf("connected: %s (%s) — %d node(s) online",
			callsign, src, r.clients.count())
	} else if r.cfg.Debug {
		log.Printf("poll: %s (%s)", callsign, src)
	}

	if r.notifier != nil {
		r.notifier.Notify()
	}

	reply := buildPollReply(r.callsign)
	if _, err := r.conn.WriteToUDP(reply, src); err != nil && r.cfg.Debug {
		log.Printf("poll reply error to %s: %v", src, err)
	}
}

// handleData processes a YSFD voice/data frame.
// In normal mode the frame is relayed verbatim to every connected node except
// the sender.  In parrot mode frames are buffered and replayed after the
// transmission ends.
func (r *Reflector) handleData(pkt []byte, src *net.UDPAddr) {
	if len(pkt) < DataSize {
		return
	}

	srcCallsign := parseDataSrcCallsign(pkt)
	srcKey := src.String()

	// Refresh the sender's last-seen timestamp.
	r.clients.upsert(src, srcCallsign)

	r.tickWatchdog(srcCallsign)

	if r.cfg.Debug {
		log.Printf("data from %s (%s)", srcCallsign, src)
	}

	if r.cfg.Parrot {
		frame := make([]byte, len(pkt))
		copy(frame, pkt)
		r.parrotMu.Lock()
		r.parrotBuf = append(r.parrotBuf, frame)
		r.parrotMu.Unlock()
		return
	}

	for _, c := range r.clients.snapshot() {
		if c.addr.String() == srcKey {
			continue // do not echo back to sender
		}
		if _, err := r.conn.WriteToUDP(pkt, c.addr); err != nil && r.cfg.Debug {
			log.Printf("relay error to %s: %v", c.addr, err)
		}
	}
}

// handleUnlink removes a node that sent a YSFU disconnect request.
func (r *Reflector) handleUnlink(pkt []byte, src *net.UDPAddr) {
	if len(pkt) < UnlinkSize {
		return
	}
	callsign := parseUnlinkCallsign(pkt)
	r.clients.remove(src.String())
	log.Printf("unlinked: %s (%s) — %d node(s) online", callsign, src, r.clients.count())
	if r.notifier != nil {
		r.notifier.Notify()
	}
}

// handleStatus responds to a YSFS status query with the reflector's ID, name,
// description, and current client count.
func (r *Reflector) handleStatus(src *net.UDPAddr) {
	reply := buildStatusReply(r.cfg, r.clients.count())
	if _, err := r.conn.WriteToUDP(reply, src); err != nil && r.cfg.Debug {
		log.Printf("status reply error to %s: %v", src, err)
	}
	if r.cfg.Debug {
		log.Printf("status query from %s — replied with count %d", src, r.clients.count())
	}
}

// tickWatchdog resets (or starts) the 1500 ms transmission-end watchdog.
// When it fires with no intervening frames the transmission is considered over.
func (r *Reflector) tickWatchdog(callsign string) {
	const watchdogDuration = 1500 * time.Millisecond

	r.watchdogMu.Lock()
	txStarting := r.watchdogCurrent == ""
	if txStarting {
		r.watchdogStarted = time.Now()
		log.Printf("transmission started: %s", callsign)
	}
	r.watchdogCurrent = callsign

	if r.watchdogTimer != nil {
		r.watchdogTimer.Reset(watchdogDuration)
		r.watchdogMu.Unlock()
		return
	}

	r.watchdogTimer = time.AfterFunc(watchdogDuration, func() {
		r.watchdogMu.Lock()
		cs := r.watchdogCurrent
		started := r.watchdogStarted
		r.watchdogCurrent = ""
		r.watchdogTimer = nil
		r.watchdogMu.Unlock()

		ended := time.Now()
		log.Printf("transmission ended: %s", cs)

		r.txMu.Lock()
		r.txLog = append([]TransmissionEntry{{
			Callsign:  cs,
			StartedAt: started,
			EndedAt:   ended,
			Duration:  ended.Sub(started),
		}}, r.txLog...)
		if len(r.txLog) > txLogMax {
			r.txLog = r.txLog[:txLogMax]
		}
		r.txMu.Unlock()

		if r.notifier != nil {
			r.notifier.Notify()
		}

		if r.cfg.Parrot {
			go r.parrotReplay()
		}
	})
	r.watchdogMu.Unlock()

	if txStarting && r.notifier != nil {
		r.notifier.Notify()
	}
}

// parrotReplay replays all buffered frames to every connected node.
// It is run in a goroutine after each transmission ends when parrot mode is on.
func (r *Reflector) parrotReplay() {
	r.parrotMu.Lock()
	frames := r.parrotBuf
	r.parrotBuf = nil
	r.parrotMu.Unlock()

	if len(frames) == 0 {
		return
	}

	log.Printf("parrot: replaying %d frame(s)", len(frames))

	// Brief pause so the transmitting node has time to switch to RX.
	time.Sleep(500 * time.Millisecond)

	for _, frame := range frames {
		for _, c := range r.clients.snapshot() {
			if _, err := r.conn.WriteToUDP(frame, c.addr); err != nil && r.cfg.Debug {
				log.Printf("parrot relay error to %s: %v", c.addr, err)
			}
		}
		// ~100 ms matches the YSF frame interval to avoid receiver buffer overrun.
		time.Sleep(100 * time.Millisecond)
	}

	log.Printf("parrot: replay complete")
}

// evictLoop periodically removes clients that have stopped polling.
func (r *Reflector) evictLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	timeout := time.Duration(r.cfg.Timeout) * time.Second

	for range ticker.C {
		evicted := r.clients.evictExpired(timeout)
		for _, cs := range evicted {
			log.Printf("disconnected (timeout): %s — %d node(s) online", cs, r.clients.count())
		}
		if len(evicted) > 0 && r.notifier != nil {
			r.notifier.Notify()
		}
	}
}

// serverPollLoop proactively polls every connected client every 5 seconds.
// This mirrors the reference implementation's health-check behaviour.
func (r *Reflector) serverPollLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	poll := buildPollReply(r.callsign)

	for range ticker.C {
		for _, c := range r.clients.snapshot() {
			if _, err := r.conn.WriteToUDP(poll, c.addr); err != nil && r.cfg.Debug {
				log.Printf("server poll error to %s: %v", c.addr, err)
			}
		}
	}
}

// statusDumpLoop logs connected node callsigns every 120 seconds.
func (r *Reflector) statusDumpLoop() {
	ticker := time.NewTicker(120 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		clients := r.clients.snapshot()
		if len(clients) == 0 {
			log.Printf("status: no nodes connected")
			continue
		}
		log.Printf("status: %d node(s) connected:", len(clients))
		for _, c := range clients {
			log.Printf("  %s (%s)", c.callsign, c.addr)
		}
	}
}
