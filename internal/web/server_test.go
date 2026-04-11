package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/siteworxpro/ysf-reflector-go/internal/config"
)

// mockProvider satisfies ClientProvider for testing.
type mockProvider struct {
	clients     []ClientInfo
	transmitter *ActiveTransmitterInfo
	txLog       []TransmissionEntryInfo
}

func (m *mockProvider) Clients() []ClientInfo                    { return m.clients }
func (m *mockProvider) ActiveTransmitter() *ActiveTransmitterInfo { return m.transmitter }
func (m *mockProvider) TransmissionLog() []TransmissionEntryInfo  { return m.txLog }

func newTestServerConfig() *config.Config {
	return &config.Config{
		Callsign:    "W1TEST",
		ID:          12345,
		Name:        "TestNet",
		Description: "Test only",
		Port:        42000,
		HTTPPort:    8080,
		Timeout:     240,
	}
}

func newTestServer(t *testing.T, p *mockProvider) *Server {
	t.Helper()
	s, err := New(newTestServerConfig(), p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// ---------- New ----------

func TestNew_Success(t *testing.T) {
	s := newTestServer(t, &mockProvider{})
	if s == nil {
		t.Fatal("New returned nil")
	}
}

// ---------- checkOrigin ----------

func TestCheckOrigin_NoOriginHeader(t *testing.T) {
	s := newTestServer(t, &mockProvider{})
	fn := s.checkOrigin(nil)

	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	if !fn(r) {
		t.Fatal("expected true for request without Origin header")
	}
}

func TestCheckOrigin_SameOrigin(t *testing.T) {
	s := newTestServer(t, &mockProvider{})
	fn := s.checkOrigin(nil)

	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Host = "example.com"
	r.Header.Set("Origin", "http://example.com")

	if !fn(r) {
		t.Fatal("expected true for same-origin request")
	}
}

func TestCheckOrigin_DifferentOrigin(t *testing.T) {
	s := newTestServer(t, &mockProvider{})
	fn := s.checkOrigin(nil)

	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Host = "example.com"
	r.Header.Set("Origin", "http://evil.com")

	if fn(r) {
		t.Fatal("expected false for cross-origin request")
	}
}

func TestCheckOrigin_InvalidOriginURL(t *testing.T) {
	s := newTestServer(t, &mockProvider{})
	fn := s.checkOrigin(nil)

	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Origin", "://not a url")

	if fn(r) {
		t.Fatal("expected false for malformed Origin URL")
	}
}

func TestCheckOrigin_AllowlistMatch(t *testing.T) {
	s := newTestServer(t, &mockProvider{})
	allowed := map[string]struct{}{
		"http://trusted.com": {},
	}
	fn := s.checkOrigin(allowed)

	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Origin", "http://trusted.com")

	if !fn(r) {
		t.Fatal("expected true for allowlisted origin")
	}
}

func TestCheckOrigin_AllowlistNoMatch(t *testing.T) {
	s := newTestServer(t, &mockProvider{})
	allowed := map[string]struct{}{
		"http://trusted.com": {},
	}
	fn := s.checkOrigin(allowed)

	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Origin", "http://other.com")

	if fn(r) {
		t.Fatal("expected false for non-allowlisted origin")
	}
}

// ---------- handleDashboard ----------

func TestHandleDashboard_Root(t *testing.T) {
	s := newTestServer(t, &mockProvider{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	s.handleDashboard(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type: got %q", ct)
	}
}

func TestHandleDashboard_NotFoundForNonRoot(t *testing.T) {
	s := newTestServer(t, &mockProvider{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/other", nil)
	s.handleDashboard(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ---------- handleAPIClients ----------

func TestHandleAPIClients_Empty(t *testing.T) {
	s := newTestServer(t, &mockProvider{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	s.handleAPIClients(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var clients []clientJSON
	if err := json.Unmarshal(w.Body.Bytes(), &clients); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(clients) != 0 {
		t.Fatalf("expected empty array, got %d", len(clients))
	}
}

func TestHandleAPIClients_WithClients(t *testing.T) {
	now := time.Now()
	p := &mockProvider{
		clients: []ClientInfo{
			{Callsign: "W1AW", Addr: "192.0.2.1:42000", LastSeen: now},
			{Callsign: "KD9ABC", Addr: "192.0.2.2:42000", LastSeen: now},
		},
	}
	s := newTestServer(t, p)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	s.handleAPIClients(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var clients []clientJSON
	if err := json.Unmarshal(w.Body.Bytes(), &clients); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(clients) != 2 {
		t.Fatalf("expected 2 clients, got %d", len(clients))
	}
	if clients[0].Callsign != "W1AW" && clients[1].Callsign != "W1AW" {
		t.Fatal("W1AW not found in response")
	}
}

func TestHandleAPIClients_ContentType(t *testing.T) {
	s := newTestServer(t, &mockProvider{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	s.handleAPIClients(w, r)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type: got %q, want application/json", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control: got %q, want no-store", cc)
	}
}

// ---------- handleAPITransmitter ----------

func TestHandleAPITransmitter_NoTransmitter(t *testing.T) {
	s := newTestServer(t, &mockProvider{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/transmitter", nil)
	s.handleAPITransmitter(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var tx transmitterJSON
	if err := json.Unmarshal(w.Body.Bytes(), &tx); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tx.OnAir {
		t.Fatal("expected on_air=false when no transmitter")
	}
}

func TestHandleAPITransmitter_Active(t *testing.T) {
	now := time.Now()
	p := &mockProvider{
		transmitter: &ActiveTransmitterInfo{Callsign: "W1AW", StartedAt: now},
	}
	s := newTestServer(t, p)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/transmitter", nil)
	s.handleAPITransmitter(w, r)

	var tx transmitterJSON
	if err := json.Unmarshal(w.Body.Bytes(), &tx); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !tx.OnAir {
		t.Fatal("expected on_air=true")
	}
	if tx.Callsign != "W1AW" {
		t.Fatalf("callsign: got %q, want W1AW", tx.Callsign)
	}
}

// ---------- handleAPITransmissions ----------

func TestHandleAPITransmissions_Empty(t *testing.T) {
	s := newTestServer(t, &mockProvider{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/transmissions", nil)
	s.handleAPITransmissions(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var entries []transmissionJSON
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty, got %d", len(entries))
	}
}

func TestHandleAPITransmissions_WithEntries(t *testing.T) {
	now := time.Now()
	p := &mockProvider{
		txLog: []TransmissionEntryInfo{
			{
				Callsign:  "W1AW",
				StartedAt: now,
				EndedAt:   now.Add(5 * time.Second),
				Duration:  5 * time.Second,
			},
		},
	}
	s := newTestServer(t, p)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/transmissions", nil)
	s.handleAPITransmissions(w, r)

	var entries []transmissionJSON
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Callsign != "W1AW" {
		t.Fatalf("callsign: got %q, want W1AW", entries[0].Callsign)
	}
	if entries[0].DurationSec != 5.0 {
		t.Fatalf("duration_sec: got %f, want 5.0", entries[0].DurationSec)
	}
}

// ---------- buildStateMessage ----------

func TestBuildStateMessage_ValidJSON(t *testing.T) {
	now := time.Now()
	p := &mockProvider{
		clients: []ClientInfo{
			{Callsign: "W1AW", Addr: "192.0.2.1:42000", LastSeen: now},
		},
		transmitter: &ActiveTransmitterInfo{Callsign: "W1AW", StartedAt: now},
		txLog: []TransmissionEntryInfo{
			{Callsign: "KD9ABC", StartedAt: now, EndedAt: now.Add(time.Second), Duration: time.Second},
		},
	}
	s := newTestServer(t, p)

	msg, err := s.buildStateMessage()
	if err != nil {
		t.Fatalf("buildStateMessage: %v", err)
	}

	var state stateMessage
	if err := json.Unmarshal(msg, &state); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(state.Clients) != 1 {
		t.Fatalf("clients: got %d, want 1", len(state.Clients))
	}
	if !state.Transmitter.OnAir {
		t.Fatal("expected transmitter on_air=true")
	}
	if len(state.Transmissions) != 1 {
		t.Fatalf("transmissions: got %d, want 1", len(state.Transmissions))
	}
}

func TestBuildStateMessage_NoTransmitter(t *testing.T) {
	s := newTestServer(t, &mockProvider{})

	msg, err := s.buildStateMessage()
	if err != nil {
		t.Fatalf("buildStateMessage: %v", err)
	}

	var state stateMessage
	if err := json.Unmarshal(msg, &state); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if state.Transmitter.OnAir {
		t.Fatal("expected on_air=false when no transmitter")
	}
}

// ---------- Notify ----------

func TestNotify_DoesNotBlock(t *testing.T) {
	s := newTestServer(t, &mockProvider{})
	// Notify should never block even if the hub channel is not being drained.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 32; i++ {
			s.Notify()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Notify blocked")
	}
}
