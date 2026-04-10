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

// Reflector is a YSF (Yaesu System Fusion) UDP reflector.
// It listens for YSFP keepalive polls and YSFD voice/data frames, maintaining
// a list of active nodes and relaying incoming frames to all other nodes.
type Reflector struct {
	cfg      *config.Config
	conn     *net.UDPConn
	clients  *clientStore
	callsign []byte // 10-byte padded

	// Transmission watchdog — tracks the currently active transmitter.
	watchdogMu      sync.Mutex
	watchdogTimer   *time.Timer
	watchdogCurrent string // callsign currently on air, empty when idle
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
	defer conn.Close()

	log.Printf("YSF reflector %s listening on UDP port %d (client timeout %ds)",
		r.cfg.Callsign, r.cfg.Port, r.cfg.Timeout)

	webSrv, err := web.New(r.cfg, r)
	if err != nil {
		return fmt.Errorf("init web server: %w", err)
	}
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

	reply := buildPollReply(r.callsign)
	if _, err := r.conn.WriteToUDP(reply, src); err != nil && r.cfg.Debug {
		log.Printf("poll reply error to %s: %v", src, err)
	}
}

// handleData processes a YSFD voice/data frame.
// Relays the frame verbatim to every connected node except the sender,
// and drives the transmission watchdog.
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
	defer r.watchdogMu.Unlock()

	if r.watchdogCurrent == "" {
		log.Printf("transmission started: %s", callsign)
	}
	r.watchdogCurrent = callsign

	if r.watchdogTimer != nil {
		r.watchdogTimer.Reset(watchdogDuration)
		return
	}

	r.watchdogTimer = time.AfterFunc(watchdogDuration, func() {
		r.watchdogMu.Lock()
		cs := r.watchdogCurrent
		r.watchdogCurrent = ""
		r.watchdogTimer = nil
		r.watchdogMu.Unlock()

		log.Printf("transmission ended: %s", cs)
	})
}

// evictLoop periodically removes clients that have stopped polling.
func (r *Reflector) evictLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	timeout := time.Duration(r.cfg.Timeout) * time.Second

	for range ticker.C {
		for _, cs := range r.clients.evictExpired(timeout) {
			log.Printf("disconnected (timeout): %s — %d node(s) online", cs, r.clients.count())
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
