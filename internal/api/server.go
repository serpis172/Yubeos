// Package api wires the HTTP surface of yubeosd: routing, middleware and handlers.
package api

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/serpis172/Yubeos/internal/clock"
	"github.com/serpis172/Yubeos/internal/config"
	"github.com/serpis172/Yubeos/internal/problem"
)

// Options are the dependencies of the API server.
type Options struct {
	Config config.Config
	Log    *slog.Logger
	Clock  clock.Clock
	Demo   bool
	UI     fs.FS // embedded web build; may be empty
}

// Server holds shared state for handlers.
type Server struct {
	opts  Options
	start time.Time
}

// New returns the fully wired http.Handler.
func New(o Options) http.Handler {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Clock == nil {
		o.Clock = clock.Real{}
	}
	s := &Server{opts: o, start: o.Clock.Now()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.Handle("GET /api/v1/system/info", s.require(RoleUser, s.systemInfo))
	// Anything else under /api/ or /ws/ is a JSON 404, never the SPA shell.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { problem.NotFound(w, r) })
	mux.HandleFunc("/ws/", func(w http.ResponseWriter, r *http.Request) { problem.NotFound(w, r) })
	mux.Handle("/", spa(o.UI))

	return chain(mux,
		requestID,
		accessLog(o.Log),
		recoverer(o.Log),
		securityHeaders,
		hostGuard(o.Config.Server.AllowedHosts),
	)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) systemInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, collectSystemInfo(s.opts.Demo, s.opts.Clock.Now(), s.start))
}
