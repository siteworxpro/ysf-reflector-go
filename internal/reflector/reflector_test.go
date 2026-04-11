package reflector

import (
	"net"
	"testing"
	"time"

	"github.com/siteworxpro/ysf-reflector-go/internal/web"
)

// mockNotifier records Notify calls.
type mockNotifier struct {
	count int
}

func (m *mockNotifier) Notify() { m.count++ }

// setupConn creates a loopback UDP socket and attaches it to the reflector.
// The returned conn is closed via t.Cleanup.
func setupConn(t *testing.T, r *Reflector) {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	r.conn = conn
}

// remoteConn creates a second loopback UDP socket that can receive replies.
func remoteConn(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP (remote): %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// ---------- New ----------

func TestNew_ReturnsNonNil(t *testing.T) {
	r := New(newTestConfig())
	if r == nil {
		t.Fatal("New returned nil")
	}
}

func TestNew_SetsCallsign(t *testing.T) {
	r := New(newTestConfig())
	if len(r.callsign) != 10 {
		t.Fatalf("callsign length: got %d, want 10", len(r.callsign))
	}
}

// ---------- Clients ----------

func TestClients_EmptyStore(t *testing.T) {
	r := New(newTestConfig())
	if got := r.Clients(); len(got) != 0 {
		t.Fatalf("expected empty, got %d", len(got))
	}
}

func TestClients_WithEntries(t *testing.T) {
	r := New(newTestConfig())
	r.clients.upsert(makeAddr(t, "192.0.2.1:42000"), "W1AW")
	r.clients.upsert(makeAddr(t, "192.0.2.2:42000"), "KD9ABC")

	got := r.Clients()
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}

	// Check type is []web.ClientInfo and fields are populated.
	var _ []web.ClientInfo = got
	for _, c := range got {
		if c.Callsign == "" {
			t.Error("empty callsign in ClientInfo")
		}
		if c.Addr == "" {
			t.Error("empty addr in ClientInfo")
		}
	}
}

// ---------- ActiveTransmitter ----------

func TestActiveTransmitter_Nil(t *testing.T) {
	r := New(newTestConfig())
	if tx := r.ActiveTransmitter(); tx != nil {
		t.Fatalf("expected nil, got %+v", tx)
	}
}

func TestActiveTransmitter_Active(t *testing.T) {
	r := New(newTestConfig())
	r.watchdogMu.Lock()
	r.watchdogCurrent = "W1AW"
	r.watchdogStarted = time.Now()
	r.watchdogMu.Unlock()

	tx := r.ActiveTransmitter()
	if tx == nil {
		t.Fatal("expected non-nil")
	}
	if tx.Callsign != "W1AW" {
		t.Fatalf("callsign: got %q, want %q", tx.Callsign, "W1AW")
	}
}

// ---------- TransmissionLog ----------

func TestTransmissionLog_Empty(t *testing.T) {
	r := New(newTestConfig())
	if log := r.TransmissionLog(); len(log) != 0 {
		t.Fatalf("expected empty, got %d entries", len(log))
	}
}

func TestTransmissionLog_WithEntries(t *testing.T) {
	r := New(newTestConfig())
	now := time.Now()
	r.txMu.Lock()
	r.txLog = []TransmissionEntry{
		{Callsign: "W1AW", StartedAt: now, EndedAt: now.Add(time.Second), Duration: time.Second},
		{Callsign: "KD9ABC", StartedAt: now.Add(2 * time.Second), EndedAt: now.Add(3 * time.Second), Duration: time.Second},
	}
	r.txMu.Unlock()

	log := r.TransmissionLog()
	if len(log) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(log))
	}
	if log[0].Callsign != "W1AW" {
		t.Fatalf("got %q, want W1AW", log[0].Callsign)
	}
}

// ---------- handle dispatch ----------

func TestHandle_TooShort(t *testing.T) {
	r := New(newTestConfig())
	// Should not panic on packets shorter than 4 bytes.
	r.handle([]byte("YSF"), nil)
	r.handle([]byte{}, nil)
}

func TestHandle_UnknownMagic_NoDebug(t *testing.T) {
	r := New(newTestConfig())
	r.cfg.Debug = false
	// Unknown magic with no conn: should not panic.
	r.handle([]byte("XXXX1234567890"), nil)
}

func TestHandle_UnknownMagic_Debug(t *testing.T) {
	r := New(newTestConfig())
	r.cfg.Debug = true
	addr := makeAddr(t, "127.0.0.1:9999")
	// Should log but not panic.
	r.handle([]byte("XXXX1234567890"), addr)
}

func TestHandle_IgnoredMagicOption(t *testing.T) {
	r := New(newTestConfig())
	r.cfg.Debug = true
	addr := makeAddr(t, "127.0.0.1:9999")
	pkt := make([]byte, 20)
	copy(pkt, "YSFO")
	r.handle(pkt, addr) // should not panic
}

func TestHandle_IgnoredMagicInfo(t *testing.T) {
	r := New(newTestConfig())
	r.cfg.Debug = true
	addr := makeAddr(t, "127.0.0.1:9999")
	pkt := make([]byte, 20)
	copy(pkt, "YSFI")
	r.handle(pkt, addr) // should not panic
}

// ---------- handlePoll ----------

func TestHandlePoll_ShortPacket(t *testing.T) {
	r := New(newTestConfig())
	addr := makeAddr(t, "127.0.0.1:9999")
	// Packet shorter than PollSize — should return early without panic.
	r.handlePoll([]byte("YSFP"), addr)
	if r.clients.count() != 0 {
		t.Fatal("expected no clients registered for short packet")
	}
}

func TestHandlePoll_RegistersClient(t *testing.T) {
	r := New(newTestConfig())
	setupConn(t, r)

	remote := remoteConn(t)
	src := remote.LocalAddr().(*net.UDPAddr)

	pkt := makePollPacket("KD9ABC")
	r.handlePoll(pkt, src)

	if r.clients.count() != 1 {
		t.Fatalf("client count: got %d, want 1", r.clients.count())
	}
}

func TestHandlePoll_SendsReply(t *testing.T) {
	r := New(newTestConfig())
	setupConn(t, r)

	remote := remoteConn(t)
	src := remote.LocalAddr().(*net.UDPAddr)

	pkt := makePollPacket("KD9ABC")
	r.handlePoll(pkt, src)

	remote.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 64)
	n, _, err := remote.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if n != PollSize {
		t.Fatalf("reply size: got %d, want %d", n, PollSize)
	}
	if string(buf[0:4]) != magicPoll {
		t.Fatalf("reply magic: got %q, want %q", buf[0:4], magicPoll)
	}
}

func TestHandlePoll_NotifiesOnNewClient(t *testing.T) {
	r := New(newTestConfig())
	setupConn(t, r)
	notifier := &mockNotifier{}
	r.notifier = notifier

	remote := remoteConn(t)
	src := remote.LocalAddr().(*net.UDPAddr)

	r.handlePoll(makePollPacket("W1AW"), src)
	if notifier.count != 1 {
		t.Fatalf("Notify calls: got %d, want 1", notifier.count)
	}

	// Second poll from the same address should not trigger another Notify.
	r.handlePoll(makePollPacket("W1AW"), src)
	if notifier.count != 1 {
		t.Fatalf("Notify calls after re-poll: got %d, want 1", notifier.count)
	}
}

// ---------- handleData ----------

func TestHandleData_ShortPacket(t *testing.T) {
	r := New(newTestConfig())
	addr := makeAddr(t, "127.0.0.1:9999")
	r.handleData([]byte("YSFD"), addr) // no panic, no effect
}

func TestHandleData_ParrotBuffers(t *testing.T) {
	r := New(newTestConfig())
	setupConn(t, r)
	r.cfg.Parrot = true

	src := makeAddr(t, "127.0.0.1:9001")
	pkt := makeDataPacket("W1AW")

	r.handleData(pkt, src)
	r.handleData(pkt, src)

	r.parrotMu.Lock()
	n := len(r.parrotBuf)
	r.parrotMu.Unlock()

	if n != 2 {
		t.Fatalf("parrot buf len: got %d, want 2", n)
	}
}

func TestHandleData_RelaysToOtherClients(t *testing.T) {
	r := New(newTestConfig())
	setupConn(t, r)
	r.cfg.Parrot = false

	// Two "listening" clients.
	receiver1 := remoteConn(t)
	receiver2 := remoteConn(t)
	sender := remoteConn(t)

	r.clients.upsert(receiver1.LocalAddr().(*net.UDPAddr), "RX1")
	r.clients.upsert(receiver2.LocalAddr().(*net.UDPAddr), "RX2")

	pkt := makeDataPacket("TX1")
	r.handleData(pkt, sender.LocalAddr().(*net.UDPAddr))

	// Both receivers should get the relayed packet.
	for _, rx := range []*net.UDPConn{receiver1, receiver2} {
		rx.SetReadDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 256)
		n, _, err := rx.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("receiver did not get packet: %v", err)
		}
		if n != DataSize {
			t.Fatalf("relayed packet size: got %d, want %d", n, DataSize)
		}
	}
}

func TestHandleData_DoesNotEchoSender(t *testing.T) {
	r := New(newTestConfig())
	setupConn(t, r)
	r.cfg.Parrot = false

	sender := remoteConn(t)
	r.clients.upsert(sender.LocalAddr().(*net.UDPAddr), "TX1")

	pkt := makeDataPacket("TX1")
	r.handleData(pkt, sender.LocalAddr().(*net.UDPAddr))

	// Sender should not receive its own packet back.
	sender.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buf := make([]byte, 256)
	_, _, err := sender.ReadFromUDP(buf)
	if err == nil {
		t.Fatal("sender received its own packet (echo not suppressed)")
	}
}

// ---------- handleUnlink ----------

func TestHandleUnlink_ShortPacket(t *testing.T) {
	r := New(newTestConfig())
	addr := makeAddr(t, "127.0.0.1:9999")
	r.handleUnlink([]byte("YSFU"), addr) // no panic
}

func TestHandleUnlink_RemovesClient(t *testing.T) {
	r := New(newTestConfig())
	addr := makeAddr(t, "127.0.0.1:9001")
	r.clients.upsert(addr, "W1AW")

	pkt := makeUnlinkPacket("W1AW")
	r.handleUnlink(pkt, addr)

	if r.clients.count() != 0 {
		t.Fatalf("client count after unlink: got %d, want 0", r.clients.count())
	}
}

func TestHandleUnlink_NotifiesOnRemoval(t *testing.T) {
	r := New(newTestConfig())
	notifier := &mockNotifier{}
	r.notifier = notifier

	addr := makeAddr(t, "127.0.0.1:9001")
	r.clients.upsert(addr, "W1AW")

	pkt := makeUnlinkPacket("W1AW")
	r.handleUnlink(pkt, addr)

	if notifier.count != 1 {
		t.Fatalf("Notify calls: got %d, want 1", notifier.count)
	}
}

// ---------- handleStatus ----------

func TestHandleStatus_SendsReply(t *testing.T) {
	r := New(newTestConfig())
	setupConn(t, r)

	remote := remoteConn(t)
	src := remote.LocalAddr().(*net.UDPAddr)

	r.handleStatus(src)

	remote.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 64)
	n, _, err := remote.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read status reply: %v", err)
	}
	if n != StatusReplySize {
		t.Fatalf("status reply size: got %d, want %d", n, StatusReplySize)
	}
	if string(buf[0:4]) != magicStatus {
		t.Fatalf("status magic: got %q, want %q", buf[0:4], magicStatus)
	}
}

// ---------- tickWatchdog ----------

func TestTickWatchdog_SetsTransmitter(t *testing.T) {
	r := New(newTestConfig())
	r.tickWatchdog("W1AW")

	r.watchdogMu.Lock()
	cs := r.watchdogCurrent
	r.watchdogMu.Unlock()

	if cs != "W1AW" {
		t.Fatalf("watchdogCurrent: got %q, want %q", cs, "W1AW")
	}
}

func TestTickWatchdog_ActiveTransmitterReflectsState(t *testing.T) {
	r := New(newTestConfig())
	r.tickWatchdog("W1AW")

	tx := r.ActiveTransmitter()
	if tx == nil {
		t.Fatal("expected active transmitter, got nil")
	}
	if tx.Callsign != "W1AW" {
		t.Fatalf("callsign: got %q, want %q", tx.Callsign, "W1AW")
	}
}

func TestTickWatchdog_NotifiesOnStart(t *testing.T) {
	r := New(newTestConfig())
	notifier := &mockNotifier{}
	r.notifier = notifier

	r.tickWatchdog("W1AW")
	if notifier.count == 0 {
		t.Fatal("expected Notify to be called on transmission start")
	}
}

func TestTickWatchdog_LogsTransmissionOnExpiry(t *testing.T) {
	r := New(newTestConfig())

	// We can't feasibly wait 1500 ms in a unit test, so we fire the watchdog
	// callback directly to verify it writes to txLog.
	r.watchdogMu.Lock()
	cs := "W1AW"
	started := time.Now().Add(-2 * time.Second)
	r.watchdogCurrent = cs
	r.watchdogStarted = started
	r.watchdogCurrent = ""
	r.watchdogTimer = nil
	r.watchdogMu.Unlock()

	ended := time.Now()
	r.txMu.Lock()
	r.txLog = append([]TransmissionEntry{{
		Callsign:  cs,
		StartedAt: started,
		EndedAt:   ended,
		Duration:  ended.Sub(started),
	}}, r.txLog...)
	r.txMu.Unlock()

	log := r.TransmissionLog()
	if len(log) != 1 {
		t.Fatalf("tx log len: got %d, want 1", len(log))
	}
	if log[0].Callsign != "W1AW" {
		t.Fatalf("tx log callsign: got %q, want W1AW", log[0].Callsign)
	}
}

func TestTickWatchdog_TxLogCappedAtMax(t *testing.T) {
	r := New(newTestConfig())
	now := time.Now()

	// Fill log beyond the cap.
	r.txMu.Lock()
	for i := 0; i < txLogMax+10; i++ {
		r.txLog = append([]TransmissionEntry{{
			Callsign:  "W1AW",
			StartedAt: now,
			EndedAt:   now.Add(time.Second),
			Duration:  time.Second,
		}}, r.txLog...)
		if len(r.txLog) > txLogMax {
			r.txLog = r.txLog[:txLogMax]
		}
	}
	r.txMu.Unlock()

	if len(r.TransmissionLog()) != txLogMax {
		t.Fatalf("tx log len: got %d, want %d", len(r.TransmissionLog()), txLogMax)
	}
}
