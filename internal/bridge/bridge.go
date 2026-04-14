package bridge

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/siteworxpro/ysf-reflector-go/internal/config"
)

const (
	keepaliveInterval = 5 * time.Second
	readDeadline      = 2 * time.Second
	bridgePollSize    = 14
	bridgePollMagic   = "YSFP"
	bridgeUnlinkMagic = "YSFU"
	bridgeDataMagic   = "YSFD"
)

// PacketInjector is implemented by the Reflector to receive YSFD frames
// that arrived from a bridged remote reflector.
type PacketInjector interface {
	InjectFromBridge(data []byte, from *net.UDPAddr)
}

// Bridge manages a connection to a single remote YSF reflector.
type Bridge struct {
	cfg      config.BridgeConfig
	callsign []byte // 10-byte space-padded
	injector PacketInjector

	mu          sync.RWMutex
	conn        *net.UDPConn
	remoteAddr  *net.UDPAddr
	connected   bool
	connectedAt time.Time
	stopCh      chan struct{}
}

func newBridge(cfg config.BridgeConfig, defaultCallsign []byte, injector PacketInjector) *Bridge {
	cs := defaultCallsign
	if cfg.Callsign != "" {
		cs = config.PaddedField(cfg.Callsign, 10)
	}
	return &Bridge{
		cfg:      cfg,
		callsign: cs,
		injector: injector,
	}
}

// Connected reports whether the bridge is currently active.
func (b *Bridge) Connected() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.connected
}

// connect opens a UDP connection to the remote reflector and starts
// keepalive and receive goroutines. No-op if already connected.
func (b *Bridge) connect() {
	b.mu.Lock()
	if b.connected {
		b.mu.Unlock()
		return
	}

	remoteAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(b.cfg.Host, fmt.Sprintf("%d", b.cfg.Port)))
	if err != nil {
		b.mu.Unlock()
		log.Printf("bridge %q: resolve %s:%d: %v", b.cfg.Name, b.cfg.Host, b.cfg.Port, err)
		return
	}

	conn, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		b.mu.Unlock()
		log.Printf("bridge %q: listen UDP: %v", b.cfg.Name, err)
		return
	}

	stopCh := make(chan struct{})
	b.conn = conn
	b.remoteAddr = remoteAddr
	b.connected = true
	b.connectedAt = time.Now()
	b.stopCh = stopCh
	b.mu.Unlock()

	log.Printf("bridge %q: connected to %s:%d", b.cfg.Name, b.cfg.Host, b.cfg.Port)

	go b.keepaliveLoop(conn, remoteAddr, stopCh)
	go b.receiveLoop(conn, remoteAddr, stopCh)
}

// disconnect sends YSFU to the remote reflector and closes the UDP connection.
// No-op if not connected.
func (b *Bridge) disconnect() {
	b.mu.Lock()
	if !b.connected {
		b.mu.Unlock()
		return
	}
	conn := b.conn
	remoteAddr := b.remoteAddr
	stopCh := b.stopCh
	b.connected = false
	b.conn = nil
	b.remoteAddr = nil
	b.mu.Unlock()

	close(stopCh)
	if _, err := conn.WriteToUDP(b.buildUnlink(), remoteAddr); err != nil {
		log.Printf("bridge %q: unlink send error: %v", b.cfg.Name, err)
	}
	_ = conn.Close()

	log.Printf("bridge %q: disconnected from %s:%d", b.cfg.Name, b.cfg.Host, b.cfg.Port)
}

func (b *Bridge) keepaliveLoop(conn *net.UDPConn, remoteAddr *net.UDPAddr, stopCh <-chan struct{}) {
	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()

	// Poll immediately on connect so the remote registers us right away.
	b.sendPoll(conn, remoteAddr)

	for {
		select {
		case <-ticker.C:
			b.sendPoll(conn, remoteAddr)
		case <-stopCh:
			return
		}
	}
}

func (b *Bridge) sendPoll(conn *net.UDPConn, remoteAddr *net.UDPAddr) {
	if _, err := conn.WriteToUDP(b.buildPoll(), remoteAddr); err != nil {
		log.Printf("bridge %q: keepalive poll error: %v", b.cfg.Name, err)
	}
}

func (b *Bridge) receiveLoop(conn *net.UDPConn, remoteAddr *net.UDPAddr, stopCh <-chan struct{}) {
	buf := make([]byte, 512)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(readDeadline))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-stopCh:
				return
			default:
				// Timeout or transient error — retry.
				continue
			}
		}
		// Only inject YSFD voice/data frames into the local reflector.
		if n < 4 || string(buf[:4]) != bridgeDataMagic {
			continue
		}
		data := make([]byte, n)
		copy(data, buf[:n])
		b.injector.InjectFromBridge(data, remoteAddr)
	}
}

// relayOutbound forwards a YSFD frame from a local node to the remote reflector.
func (b *Bridge) relayOutbound(data []byte) {
	b.mu.RLock()
	conn := b.conn
	remoteAddr := b.remoteAddr
	connected := b.connected
	b.mu.RUnlock()

	if !connected || conn == nil {
		return
	}
	if _, err := conn.WriteToUDP(data, remoteAddr); err != nil {
		log.Printf("bridge %q: outbound relay error: %v", b.cfg.Name, err)
	}
}

func (b *Bridge) buildPoll() []byte {
	pkt := make([]byte, bridgePollSize)
	copy(pkt[0:4], bridgePollMagic)
	copy(pkt[4:14], b.callsign)
	return pkt
}

func (b *Bridge) buildUnlink() []byte {
	pkt := make([]byte, bridgePollSize)
	copy(pkt[0:4], bridgeUnlinkMagic)
	copy(pkt[4:14], b.callsign)
	return pkt
}
