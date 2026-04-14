package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"github.com/siteworxpro/ysf-reflector-go/internal/config"
)

//go:embed templates/dashboard.html
var templateFS embed.FS

// ClientInfo holds the exported view of a connected YSF node.
type ClientInfo struct {
	Callsign string
	Addr     string
	LastSeen time.Time
}

// ActiveTransmitterInfo describes the node currently on air.
type ActiveTransmitterInfo struct {
	Callsign  string
	StartedAt time.Time
}

// TransmissionEntryInfo records a completed transmission.
type TransmissionEntryInfo struct {
	Callsign  string
	StartedAt time.Time
	EndedAt   time.Time
	Duration  time.Duration
}

// BridgeInfo holds the exported view of a configured bridge connection.
type BridgeInfo struct {
	Name        string    `json:"name"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	Connected   bool      `json:"connected"`
	ConnectedAt time.Time `json:"connected_at,omitempty"`
	Schedule    string    `json:"schedule,omitempty"`
}

// BridgeProvider is optionally implemented to supply bridge status to the dashboard.
type BridgeProvider interface {
	Bridges() []BridgeInfo
}

// ClientProvider is implemented by the Reflector to supply live client snapshots.
type ClientProvider interface {
	Clients() []ClientInfo
	ActiveTransmitter() *ActiveTransmitterInfo
	TransmissionLog() []TransmissionEntryInfo
}

// wsClient is a single WebSocket connection managed by the hub.
type wsClient struct {
	conn *websocket.Conn
	send chan []byte
}

// hub manages the set of active WebSocket connections and broadcasts messages.
type hub struct {
	clients    map[*wsClient]struct{}
	broadcast  chan []byte
	register   chan *wsClient
	unregister chan *wsClient
}

func newHub() *hub {
	return &hub{
		clients:    make(map[*wsClient]struct{}),
		broadcast:  make(chan []byte, 16),
		register:   make(chan *wsClient),
		unregister: make(chan *wsClient),
	}
}

func (h *hub) run() {
	for {
		select {
		case c := <-h.register:
			h.clients[c] = struct{}{}
		case c := <-h.unregister:
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
			}
		case msg := <-h.broadcast:
			for c := range h.clients {
				select {
				case c.send <- msg:
				default:
					// Slow client: drop and disconnect.
					delete(h.clients, c)
					close(c.send)
				}
			}
		}
	}
}

// writePump pumps messages from the send channel to the WebSocket connection.
func (c *wsClient) writePump(h *hub) {
	const pingInterval = 30 * time.Second
	const writeTimeout = 10 * time.Second

	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if !ok {
				// Hub closed the channel.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump drains the connection and detects disconnects.
func (c *wsClient) readPump(h *hub) {
	defer func() {
		h.unregister <- c
		_ = c.conn.Close()
	}()

	const maxMsg = 512
	const pongTimeout = 70 * time.Second

	c.conn.SetReadLimit(maxMsg)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongTimeout))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongTimeout))
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			break
		}
	}
}

// Server serves the dashboard and API endpoints.
type Server struct {
	cfg            *config.Config
	provider       ClientProvider
	bridgeProvider BridgeProvider // optional
	tmpl           *template.Template
	hub            *hub
	upgrader       websocket.Upgrader
}

// New creates a Server. It parses the embedded dashboard template at startup so
// any template errors are caught immediately.
func New(cfg *config.Config, provider ClientProvider) (*Server, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/dashboard.html")
	if err != nil {
		return nil, fmt.Errorf("parse dashboard template: %w", err)
	}
	h := newHub()
	go h.run()

	// Build the allowlist set from config (case-normalised scheme+host).
	allowed := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		if u, err := url.Parse(o); err == nil {
			allowed[u.Scheme+"://"+u.Host] = struct{}{}
		}
	}

	s := &Server{
		cfg:      cfg,
		provider: provider,
		tmpl:     tmpl,
		hub:      h,
	}
	s.upgrader = websocket.Upgrader{
		ReadBufferSize:  256,
		WriteBufferSize: 4096,
		CheckOrigin:     s.checkOrigin(allowed),
	}
	return s, nil
}

// checkOrigin returns a CheckOrigin function that enforces same-origin policy.
// If the caller configured an explicit allowlist it is consulted first; otherwise
// the Origin header must match the Host header of the incoming request.
// Requests without an Origin header (e.g. native clients, curl) are permitted.
func (s *Server) checkOrigin(allowed map[string]struct{}) func(*http.Request) bool {
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Non-browser clients do not send Origin; allow them.
			return true
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}

		// Explicit allowlist takes priority when configured.
		if len(allowed) > 0 {
			_, ok := allowed[u.Scheme+"://"+u.Host]
			return ok
		}

		// Default: same-origin — the Origin host must equal the Host header.
		return u.Host == r.Host
	}
}

// heartbeat broadcasts the current reflector state at a fixed interval so that
// last_seen timestamps remain fresh enough for clients to derive Active/Idle
// status without the server needing to push on every keepalive poll.
func (s *Server) heartbeat() {
	const interval = 15 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		s.Notify()
	}
}

// Notify builds the current reflector state and broadcasts it to all connected
// WebSocket clients. It is safe to call from any goroutine and never blocks.
func (s *Server) Notify() {
	msg, err := s.buildStateMessage()
	if err != nil {
		log.Printf("ws notify: build state: %v", err)
		return
	}
	select {
	case s.hub.broadcast <- msg:
	default:
		// Hub channel full — drop; clients will receive the next notification.
	}
}

// SetBridgeProvider registers an optional bridge status provider.
// Must be called before ListenAndServe.
func (s *Server) SetBridgeProvider(bp BridgeProvider) {
	s.bridgeProvider = bp
}

// ListenAndServe starts the HTTP server and blocks until it returns an error.
// A background goroutine periodically broadcasts the current state so that
// client-side "Active/Idle" badges (derived from last_seen) stay fresh without
// requiring a push on every keepalive poll.
func (s *Server) ListenAndServe() error {
	go s.heartbeat()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/api/clients", s.handleAPIClients)
	mux.HandleFunc("/api/transmitter", s.handleAPITransmitter)
	mux.HandleFunc("/api/transmissions", s.handleAPITransmissions)
	mux.HandleFunc("/api/bridges", s.handleAPIBridges)
	mux.HandleFunc("/", s.handleDashboard)

	addr := fmt.Sprintf(":%d", s.cfg.HTTPPort)
	log.Printf("dashboard listening on http://0.0.0.0%s", addr)

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return srv.ListenAndServe()
}

// dashboardData is passed to the HTML template.
type dashboardData struct {
	Callsign    string
	Id          uint32
	Port        int
	Name        string
	Description string
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data := dashboardData{
		Callsign:    s.cfg.Callsign,
		Id:          s.cfg.ID,
		Port:        s.cfg.Port,
		Name:        s.cfg.Name,
		Description: s.cfg.Description,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.Execute(w, data); err != nil {
		log.Printf("dashboard template error: %v", err)
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}

	client := &wsClient{conn: conn, send: make(chan []byte, 16)}
	s.hub.register <- client

	// Send current state immediately so the new client doesn't have to wait.
	if msg, err := s.buildStateMessage(); err == nil {
		select {
		case client.send <- msg:
		default:
		}
	}

	go client.writePump(s.hub)
	go client.readPump(s.hub)
}

// stateMessage is the JSON payload pushed to WebSocket clients on every change.
type stateMessage struct {
	Clients       []clientJSON       `json:"clients"`
	Transmitter   transmitterJSON    `json:"transmitter"`
	Transmissions []transmissionJSON `json:"transmissions"`
	Bridges       []BridgeInfo       `json:"bridges"`
}

func (s *Server) buildStateMessage() ([]byte, error) {
	clients := s.provider.Clients()
	clientsOut := make([]clientJSON, 0, len(clients))
	for _, c := range clients {
		clientsOut = append(clientsOut, clientJSON(c))
	}

	var txOut transmitterJSON
	if tx := s.provider.ActiveTransmitter(); tx != nil {
		txOut = transmitterJSON{Callsign: tx.Callsign, StartedAt: tx.StartedAt, OnAir: true}
	}

	entries := s.provider.TransmissionLog()
	txLogOut := make([]transmissionJSON, 0, len(entries))
	for _, e := range entries {
		txLogOut = append(txLogOut, transmissionJSON{
			Callsign:    e.Callsign,
			StartedAt:   e.StartedAt,
			EndedAt:     e.EndedAt,
			DurationSec: e.Duration.Seconds(),
		})
	}

	var bridgesOut []BridgeInfo
	if s.bridgeProvider != nil {
		bridgesOut = s.bridgeProvider.Bridges()
	}
	if bridgesOut == nil {
		bridgesOut = []BridgeInfo{}
	}

	return json.Marshal(stateMessage{
		Clients:       clientsOut,
		Transmitter:   txOut,
		Transmissions: txLogOut,
		Bridges:       bridgesOut,
	})
}

// clientJSON is the JSON shape returned by /api/clients and WS state messages.
type clientJSON struct {
	Callsign string    `json:"callsign"`
	Addr     string    `json:"addr"`
	LastSeen time.Time `json:"last_seen"`
}

func (s *Server) handleAPIClients(w http.ResponseWriter, r *http.Request) {
	clients := s.provider.Clients()
	out := make([]clientJSON, 0, len(clients))
	for _, c := range clients {
		out = append(out, clientJSON(c))
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("api/clients encode error: %v", err)
	}
}

// transmitterJSON is the JSON shape returned by /api/transmitter and WS state messages.
type transmitterJSON struct {
	Callsign  string    `json:"callsign"`
	StartedAt time.Time `json:"started_at"`
	OnAir     bool      `json:"on_air"`
}

func (s *Server) handleAPITransmitter(w http.ResponseWriter, r *http.Request) {
	var out transmitterJSON
	if tx := s.provider.ActiveTransmitter(); tx != nil {
		out = transmitterJSON{Callsign: tx.Callsign, StartedAt: tx.StartedAt, OnAir: true}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("api/transmitter encode error: %v", err)
	}
}

// transmissionJSON is the JSON shape for a single entry in /api/transmissions and WS state messages.
type transmissionJSON struct {
	Callsign    string    `json:"callsign"`
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at"`
	DurationSec float64   `json:"duration_sec"`
}

func (s *Server) handleAPITransmissions(w http.ResponseWriter, r *http.Request) {
	entries := s.provider.TransmissionLog()
	out := make([]transmissionJSON, 0, len(entries))
	for _, e := range entries {
		out = append(out, transmissionJSON{
			Callsign:    e.Callsign,
			StartedAt:   e.StartedAt,
			EndedAt:     e.EndedAt,
			DurationSec: e.Duration.Seconds(),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("api/transmissions encode error: %v", err)
	}
}

func (s *Server) handleAPIBridges(w http.ResponseWriter, _ *http.Request) {
	var out []BridgeInfo
	if s.bridgeProvider != nil {
		out = s.bridgeProvider.Bridges()
	}
	if out == nil {
		out = []BridgeInfo{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("api/bridges encode error: %v", err)
	}
}
