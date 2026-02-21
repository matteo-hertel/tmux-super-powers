package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/matteo-hertel/tmux-super-powers/internal/service"
)

//go:embed web/index.html
var indexHTML []byte

// Server is the HTTP/WebSocket API server.
type Server struct {
	cfg      *config.Config
	monitor  *service.Monitor
	upgrader websocket.Upgrader
	httpSrv  *http.Server
}

// New creates a new API server.
func New(cfg *config.Config) *Server {
	return &Server{
		cfg: cfg,
		monitor: service.NewMonitor(
			cfg.Serve.RefreshMs,
			cfg.Dash.ErrorPatterns,
			cfg.Dash.PromptPattern,
		),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// Start starts the monitor and HTTP server.
func (s *Server) Start(bind string, port int) error {
	s.monitor.Start()

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	addr := fmt.Sprintf("%s:%d", bind, port)
	s.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      withLogging(withCORS(mux)),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("tsp serve listening on %s", addr)
	return s.httpSrv.ListenAndServe()
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	s.monitor.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Web UI
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write(indexHTML)
	})

	// System
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/config", s.handleConfig)

	// Sessions
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("GET /api/sessions/{name}", s.handleGetSession)
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("DELETE /api/sessions/{name}", s.handleDeleteSession)
	mux.HandleFunc("POST /api/sessions/{name}/send", s.handleSendToPane)

	// Spawn
	mux.HandleFunc("POST /api/spawn", s.handleSpawn)

	// PR/CI
	mux.HandleFunc("GET /api/sessions/{name}/pr", s.handleGetPR)
	mux.HandleFunc("POST /api/sessions/{name}/pr", s.handleCreatePR)
	mux.HandleFunc("POST /api/sessions/{name}/fix-ci", s.handleFixCI)
	mux.HandleFunc("POST /api/sessions/{name}/fix-reviews", s.handleFixReviews)
	mux.HandleFunc("POST /api/sessions/{name}/merge", s.handleMerge)

	// WebSocket
	mux.HandleFunc("GET /api/ws", s.handleWebSocket)
}

// DetectTailscaleIP finds the Tailscale interface IP (100.64.0.0/10 CGNAT range).
func DetectTailscaleIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.To4() == nil {
				continue
			}
			// Tailscale CGNAT range: 100.64.0.0/10
			if ip.To4()[0] == 100 && ip.To4()[1] >= 64 && ip.To4()[1] <= 127 {
				return ip.String()
			}
		}
	}
	return ""
}

// Middleware: CORS
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Middleware: request logging
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ResolveBindAddress determines the bind address.
// Priority: explicit bind flag > Tailscale IP > localhost
func ResolveBindAddress(bind string) string {
	if bind != "" {
		return bind
	}
	if tsIP := DetectTailscaleIP(); tsIP != "" {
		log.Printf("Tailscale detected, binding to %s", tsIP)
		return tsIP
	}
	log.Println("WARNING: Tailscale not detected, binding to 127.0.0.1")
	return "127.0.0.1"
}

// ParseSessionName extracts the session name from the request path.
func ParseSessionName(r *http.Request) string {
	name := r.PathValue("name")
	return strings.ReplaceAll(name, "%20", " ")
}
