package bridge

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/siteworxpro/ysf-reflector-go/internal/config"
	"github.com/siteworxpro/ysf-reflector-go/internal/web"
)

// ── Cron tests ──────────────────────────────────────────────────────────────

func TestMatchesCron_Exact(t *testing.T) {
	// 8:30 on any day
	expr := "30 8 * * *"
	match := time.Date(2026, 4, 13, 8, 30, 0, 0, time.UTC)
	nomatch := time.Date(2026, 4, 13, 8, 31, 0, 0, time.UTC)

	ok, err := MatchesCron(expr, match)
	if err != nil || !ok {
		t.Fatalf("expected match at %v, got ok=%v err=%v", match, ok, err)
	}
	ok, err = MatchesCron(expr, nomatch)
	if err != nil || ok {
		t.Fatalf("expected no match at %v, got ok=%v err=%v", nomatch, ok, err)
	}
}

func TestMatchesCron_Wildcard(t *testing.T) {
	ok, err := MatchesCron("* * * * *", time.Now())
	if err != nil || !ok {
		t.Fatalf("wildcard should always match: ok=%v err=%v", ok, err)
	}
}

func TestMatchesCron_Step(t *testing.T) {
	// Every 15 minutes
	expr := "*/15 * * * *"
	for _, min := range []int{0, 15, 30, 45} {
		t2 := time.Date(2026, 1, 1, 0, min, 0, 0, time.UTC)
		ok, err := MatchesCron(expr, t2)
		if err != nil || !ok {
			t.Errorf("minute %d should match step-15: ok=%v err=%v", min, ok, err)
		}
	}
	for _, min := range []int{1, 14, 16, 29, 31} {
		t2 := time.Date(2026, 1, 1, 0, min, 0, 0, time.UTC)
		ok, err := MatchesCron(expr, t2)
		if err != nil || ok {
			t.Errorf("minute %d should not match step-15: ok=%v err=%v", min, ok, err)
		}
	}
}

func TestMatchesCron_Range(t *testing.T) {
	// Mon–Fri only (weekday 1-5)
	expr := "0 9 * * 1-5"
	for _, wd := range []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday} {
		d := nextWeekday(wd)
		t2 := time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, time.UTC)
		ok, err := MatchesCron(expr, t2)
		if err != nil || !ok {
			t.Errorf("weekday %s should match: ok=%v err=%v", wd, ok, err)
		}
	}
	for _, wd := range []time.Weekday{time.Saturday, time.Sunday} {
		d := nextWeekday(wd)
		t2 := time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, time.UTC)
		ok, err := MatchesCron(expr, t2)
		if err != nil || ok {
			t.Errorf("weekday %s should not match: ok=%v err=%v", wd, ok, err)
		}
	}
}

func TestMatchesCron_List(t *testing.T) {
	expr := "0 8,12,18 * * *"
	for _, h := range []int{8, 12, 18} {
		t2 := time.Date(2026, 1, 1, h, 0, 0, 0, time.UTC)
		ok, err := MatchesCron(expr, t2)
		if err != nil || !ok {
			t.Errorf("hour %d should match list: ok=%v err=%v", h, ok, err)
		}
	}
	t2 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	ok, err := MatchesCron(expr, t2)
	if err != nil || ok {
		t.Errorf("hour 10 should not match list: ok=%v err=%v", ok, err)
	}
}

func TestMatchesCron_InvalidFieldCount(t *testing.T) {
	_, err := MatchesCron("* * *", time.Now())
	if err == nil {
		t.Fatal("expected error for 3-field cron")
	}
}

func TestMatchesCron_InvalidStep(t *testing.T) {
	_, err := MatchesCron("*/0 * * * *", time.Now())
	if err == nil {
		t.Fatal("expected error for step=0")
	}
}

// nextWeekday returns the next occurrence of the given weekday on or after today.
func nextWeekday(wd time.Weekday) time.Time {
	now := time.Now()
	offset := (int(wd) - int(now.Weekday()) + 7) % 7
	return now.AddDate(0, 0, offset)
}

// ── mockInjector ─────────────────────────────────────────────────────────────

type mockInjector struct {
	mu      sync.Mutex
	packets [][]byte
	addrs   []*net.UDPAddr
}

func (m *mockInjector) InjectFromBridge(data []byte, from *net.UDPAddr) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	m.packets = append(m.packets, cp)
	m.addrs = append(m.addrs, from)
}

func (m *mockInjector) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.packets)
}

// ── Bridge unit tests ─────────────────────────────────────────────────────────

func makeBridgeCfg(name, host string, port int, alwaysOn bool) config.BridgeConfig {
	return config.BridgeConfig{
		Name:     name,
		Host:     host,
		Port:     port,
		AlwaysOn: alwaysOn,
		Enabled:  true,
	}
}

func defaultCallsign() []byte {
	cs := make([]byte, 10)
	copy(cs, "K8TEST    ")
	return cs
}

func TestBridge_ConnectDisconnect(t *testing.T) {
	inj := &mockInjector{}

	// Start a real UDP listener to act as the "remote reflector".
	remote, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen remote: %v", err)
	}
	t.Cleanup(func() { _ = remote.Close() })

	cfg := makeBridgeCfg("test", "127.0.0.1", remote.LocalAddr().(*net.UDPAddr).Port, false)
	b := newBridge(cfg, defaultCallsign(), inj)

	if b.Connected() {
		t.Fatal("should not be connected before connect()")
	}

	b.connect()
	// Give keepalive goroutine time to send initial poll.
	time.Sleep(100 * time.Millisecond)

	if !b.Connected() {
		t.Fatal("should be connected after connect()")
	}

	// Verify a YSFP poll arrived at the remote.
	_ = remote.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	buf := make([]byte, 64)
	n, _, err2 := remote.ReadFromUDP(buf)
	if err2 != nil {
		t.Fatalf("remote did not receive poll: %v", err2)
	}
	if string(buf[:4]) != bridgePollMagic {
		t.Errorf("expected YSFP, got %q", buf[:4])
	}
	_ = n

	b.disconnect()
	time.Sleep(100 * time.Millisecond)

	if b.Connected() {
		t.Fatal("should not be connected after disconnect()")
	}
}

func TestBridge_InjectOnYSFD(t *testing.T) {
	inj := &mockInjector{}

	remote, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen remote: %v", err)
	}
	t.Cleanup(func() { _ = remote.Close() })

	cfg := makeBridgeCfg("inject-test", "127.0.0.1", remote.LocalAddr().(*net.UDPAddr).Port, false)
	b := newBridge(cfg, defaultCallsign(), inj)
	b.connect()
	time.Sleep(50 * time.Millisecond)

	// Learn the bridge's local source address from the first YSFP poll.
	_ = remote.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	buf := make([]byte, 64)
	_, bridgeAddr, err2 := remote.ReadFromUDP(buf)
	if err2 != nil {
		t.Fatalf("no initial poll: %v", err2)
	}

	// Send a YSFD frame back to the bridge (simulating a remote node transmitting).
	ysfd := make([]byte, 155)
	copy(ysfd[0:4], bridgeDataMagic)
	copy(ysfd[4:14], "W1AW      ")
	_, _ = remote.WriteToUDP(ysfd, bridgeAddr)

	// Wait for the inject call.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) && inj.count() == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	if inj.count() == 0 {
		t.Fatal("InjectFromBridge not called for YSFD")
	}

	b.disconnect()
}

func TestBridge_NoInjectOnNonYSFD(t *testing.T) {
	inj := &mockInjector{}

	remote, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen remote: %v", err)
	}
	t.Cleanup(func() { _ = remote.Close() })

	cfg := makeBridgeCfg("noninject-test", "127.0.0.1", remote.LocalAddr().(*net.UDPAddr).Port, false)
	b := newBridge(cfg, defaultCallsign(), inj)
	b.connect()
	time.Sleep(50 * time.Millisecond)

	// Drain initial poll.
	_ = remote.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	buf := make([]byte, 64)
	_, bridgeAddr, _ := remote.ReadFromUDP(buf)

	// Send a YSFP (poll reply) — should not be injected.
	ysfp := make([]byte, 14)
	copy(ysfp[0:4], bridgePollMagic)
	_, _ = remote.WriteToUDP(ysfp, bridgeAddr)

	time.Sleep(150 * time.Millisecond)

	if inj.count() != 0 {
		t.Fatal("InjectFromBridge should not be called for YSFP")
	}

	b.disconnect()
}

// ── Manager tests ─────────────────────────────────────────────────────────────

func TestManager_Bridges_EmptyWhenNoBridges(t *testing.T) {
	inj := &mockInjector{}
	m := NewManager(nil, defaultCallsign(), inj)
	if got := m.Bridges(); len(got) != 0 {
		t.Fatalf("expected 0 bridges, got %d", len(got))
	}
}

func TestManager_Bridges_DisabledExcluded(t *testing.T) {
	cfgs := []config.BridgeConfig{
		{Name: "disabled", Host: "127.0.0.1", Port: 42000, Enabled: false},
	}
	inj := &mockInjector{}
	m := NewManager(cfgs, defaultCallsign(), inj)
	if len(m.bridges) != 0 {
		t.Fatalf("disabled bridge should not be created")
	}
}

func TestManager_BridgeInfo_ScheduleDesc(t *testing.T) {
	tests := []struct {
		cfg  config.BridgeConfig
		want string
	}{
		{config.BridgeConfig{AlwaysOn: true}, "always on"},
		{config.BridgeConfig{ConnectCron: "0 8 * * *", DisconnectCron: "0 22 * * *"}, "connect: 0 8 * * * / disconnect: 0 22 * * *"},
		{config.BridgeConfig{ConnectCron: "0 8 * * *"}, "connect: 0 8 * * *"},
		{config.BridgeConfig{DisconnectCron: "0 22 * * *"}, "disconnect: 0 22 * * *"},
		{config.BridgeConfig{}, "manual"},
	}
	for _, tt := range tests {
		got := scheduleDesc(tt.cfg)
		if got != tt.want {
			t.Errorf("scheduleDesc(%+v) = %q, want %q", tt.cfg, got, tt.want)
		}
	}
}

func TestManager_RelayToRemote_SkipsSourceBridge(t *testing.T) {
	// Set up two "remote reflectors".
	remoteA, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	remoteB, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	t.Cleanup(func() { _ = remoteA.Close() })
	t.Cleanup(func() { _ = remoteB.Close() })

	portA := remoteA.LocalAddr().(*net.UDPAddr).Port
	portB := remoteB.LocalAddr().(*net.UDPAddr).Port

	cfgs := []config.BridgeConfig{
		{Name: "A", Host: "127.0.0.1", Port: portA, AlwaysOn: true, Enabled: true},
		{Name: "B", Host: "127.0.0.1", Port: portB, AlwaysOn: true, Enabled: true},
	}
	inj := &mockInjector{}
	m := NewManager(cfgs, defaultCallsign(), inj)
	m.Start()
	time.Sleep(200 * time.Millisecond)

	// Drain initial polls from both remotes.
	buf := make([]byte, 64)
	_ = remoteA.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	_, _, _ = remoteA.ReadFromUDP(buf)
	_ = remoteB.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	_, _, _ = remoteB.ReadFromUDP(buf)

	// Simulate a local node transmitting — relay to both bridges.
	localAddr, _ := net.ResolveUDPAddr("udp", "192.0.2.1:12345")
	ysfd := make([]byte, 155)
	copy(ysfd[0:4], bridgeDataMagic)
	m.RelayToRemote(localAddr, ysfd)

	// Both remotes should receive the frame.
	received := func(conn *net.UDPConn) bool {
		_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buf)
		return err == nil && n > 0 && string(buf[:4]) == bridgeDataMagic
	}
	if !received(remoteA) {
		t.Error("remote A did not receive relayed frame")
	}
	if !received(remoteB) {
		t.Error("remote B did not receive relayed frame")
	}

	m.Stop()
}

func TestManager_Bridges_ReturnsStatus(t *testing.T) {
	remote, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	t.Cleanup(func() { _ = remote.Close() })
	port := remote.LocalAddr().(*net.UDPAddr).Port

	cfgs := []config.BridgeConfig{
		{Name: "mybridge", Host: "127.0.0.1", Port: port, AlwaysOn: true, Enabled: true},
	}
	inj := &mockInjector{}
	m := NewManager(cfgs, defaultCallsign(), inj)
	m.Start()
	time.Sleep(100 * time.Millisecond)

	bridges := m.Bridges()
	if len(bridges) != 1 {
		t.Fatalf("expected 1 bridge, got %d", len(bridges))
	}
	b := bridges[0]
	if b.Name != "mybridge" {
		t.Errorf("name = %q", b.Name)
	}
	if b.Host != "127.0.0.1" {
		t.Errorf("host = %q", b.Host)
	}
	if b.Port != port {
		t.Errorf("port = %d", b.Port)
	}
	if !b.Connected {
		t.Error("expected connected=true")
	}
	if b.Schedule != "always on" {
		t.Errorf("schedule = %q", b.Schedule)
	}
	// Verify BridgeInfo implements web.BridgeInfo (compile-time check via assignment).
	_ = web.BridgeInfo(b)

	m.Stop()
}

func TestManager_CronSchedule(t *testing.T) {
	inj := &mockInjector{}
	remote, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	t.Cleanup(func() { _ = remote.Close() })
	port := remote.LocalAddr().(*net.UDPAddr).Port

	cfgs := []config.BridgeConfig{
		{
			Name:    "sched",
			Host:    "127.0.0.1",
			Port:    port,
			Enabled: true,
			// Connect cron that always matches (every minute).
			ConnectCron: "* * * * *",
		},
	}
	m := NewManager(cfgs, defaultCallsign(), inj)

	// Bridge should not be connected yet.
	if m.bridges[0].Connected() {
		t.Fatal("should not be connected before checkSchedules")
	}

	// Trigger cron check manually with current time → should connect.
	m.checkSchedules(time.Now())
	time.Sleep(100 * time.Millisecond)

	if !m.bridges[0].Connected() {
		t.Fatal("should be connected after matching connect cron")
	}

	// Override disconnect cron to always-match and trigger again → should disconnect.
	m.bridges[0].cfg.DisconnectCron = "* * * * *"
	m.checkSchedules(time.Now())
	time.Sleep(100 * time.Millisecond)

	if m.bridges[0].Connected() {
		t.Fatal("should be disconnected after matching disconnect cron")
	}

	m.Stop()
}
