package bridge

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/siteworxpro/ysf-reflector-go/internal/config"
	"github.com/siteworxpro/ysf-reflector-go/internal/web"
)

// Manager manages all configured bridges and their cron-based connect/disconnect schedules.
type Manager struct {
	bridges []*Bridge
	mu      sync.RWMutex
	stopCh  chan struct{}
}

// NewManager creates a Manager from the supplied bridge configs.
// defaultCallsign is the 10-byte padded reflector callsign used when a bridge
// does not override its own callsign.
// injector is the local reflector that receives inbound bridge frames.
func NewManager(cfgs []config.BridgeConfig, defaultCallsign []byte, injector PacketInjector) *Manager {
	m := &Manager{stopCh: make(chan struct{})}
	for _, cfg := range cfgs {
		if !cfg.Enabled {
			continue
		}
		m.bridges = append(m.bridges, newBridge(cfg, defaultCallsign, injector))
	}
	return m
}

// Start connects always-on bridges immediately and launches the cron scheduler.
func (m *Manager) Start() {
	for _, b := range m.bridges {
		if b.cfg.AlwaysOn {
			b.connect()
		}
	}
	go m.cronLoop()
}

// Stop disconnects all bridges and shuts down the scheduler.
func (m *Manager) Stop() {
	close(m.stopCh)
	for _, b := range m.bridges {
		b.disconnect()
	}
}

// RelayToRemote implements reflector.BridgeRelayer.
// Forwards pkt to every connected bridge except the one whose remoteAddr
// matches src (prevents echo when a packet originated from a bridge).
func (m *Manager) RelayToRemote(src *net.UDPAddr, pkt []byte) {
	srcStr := src.String()
	for _, b := range m.bridges {
		b.mu.RLock()
		var remoteStr string
		if b.remoteAddr != nil {
			remoteStr = b.remoteAddr.String()
		}
		b.mu.RUnlock()
		if remoteStr == srcStr {
			continue // packet originated from this bridge — do not echo
		}
		b.relayOutbound(pkt)
	}
}

// Bridges implements web.BridgeProvider.
func (m *Manager) Bridges() []web.BridgeInfo {
	out := make([]web.BridgeInfo, 0, len(m.bridges))
	for _, b := range m.bridges {
		b.mu.RLock()
		info := web.BridgeInfo{
			Name:        b.cfg.Name,
			Host:        b.cfg.Host,
			Port:        b.cfg.Port,
			Connected:   b.connected,
			ConnectedAt: b.connectedAt,
			Schedule:    scheduleDesc(b.cfg),
		}
		b.mu.RUnlock()
		out = append(out, info)
	}
	return out
}

func scheduleDesc(cfg config.BridgeConfig) string {
	switch {
	case cfg.AlwaysOn:
		return "always on"
	case cfg.ConnectCron != "" && cfg.DisconnectCron != "":
		return fmt.Sprintf("connect: %s / disconnect: %s", cfg.ConnectCron, cfg.DisconnectCron)
	case cfg.ConnectCron != "":
		return fmt.Sprintf("connect: %s", cfg.ConnectCron)
	case cfg.DisconnectCron != "":
		return fmt.Sprintf("disconnect: %s", cfg.DisconnectCron)
	default:
		return "manual"
	}
}

func (m *Manager) cronLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case t := <-ticker.C:
			m.checkSchedules(t)
		case <-m.stopCh:
			return
		}
	}
}

func (m *Manager) checkSchedules(t time.Time) {
	for _, b := range m.bridges {
		if b.cfg.ConnectCron != "" && !b.Connected() {
			if ok, err := MatchesCron(b.cfg.ConnectCron, t); err != nil {
				log.Printf("bridge %q: invalid connect cron %q: %v", b.cfg.Name, b.cfg.ConnectCron, err)
			} else if ok {
				b.connect()
			}
		}
		if b.cfg.DisconnectCron != "" && b.Connected() {
			if ok, err := MatchesCron(b.cfg.DisconnectCron, t); err != nil {
				log.Printf("bridge %q: invalid disconnect cron %q: %v", b.cfg.Name, b.cfg.DisconnectCron, err)
			} else if ok {
				b.disconnect()
			}
		}
	}
}
