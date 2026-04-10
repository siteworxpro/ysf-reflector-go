package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

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

// ClientProvider is implemented by the Reflector to supply live client snapshots.
type ClientProvider interface {
	Clients() []ClientInfo
}

// Server serves the dashboard and API endpoints.
type Server struct {
	cfg      *config.Config
	provider ClientProvider
	tmpl     *template.Template
}

// New creates a Server. It parses the embedded dashboard template at startup so
// any template errors are caught immediately.
func New(cfg *config.Config, provider ClientProvider) (*Server, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/dashboard.html")
	if err != nil {
		return nil, fmt.Errorf("parse dashboard template: %w", err)
	}
	return &Server{cfg: cfg, provider: provider, tmpl: tmpl}, nil
}

// ListenAndServe starts the HTTP server and blocks until it returns an error.
func (s *Server) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/clients", s.handleAPIClients)
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

// clientJSON is the JSON shape returned by /api/clients.
type clientJSON struct {
	Callsign string    `json:"callsign"`
	Addr     string    `json:"addr"`
	LastSeen time.Time `json:"last_seen"`
}

func (s *Server) handleAPIClients(w http.ResponseWriter, r *http.Request) {
	clients := s.provider.Clients()
	out := make([]clientJSON, 0, len(clients))
	for _, c := range clients {
		out = append(out, clientJSON{
			Callsign: c.Callsign,
			Addr:     c.Addr,
			LastSeen: c.LastSeen,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("api/clients encode error: %v", err)
	}
}
