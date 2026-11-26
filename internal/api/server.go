// Package api wires the HTTP surface of yubeosd: routing, middleware and handlers.
package api

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/serpis172/Yubeos/internal/addons"
	"github.com/serpis172/Yubeos/internal/auth"
	"github.com/serpis172/Yubeos/internal/clock"
	"github.com/serpis172/Yubeos/internal/config"
	"github.com/serpis172/Yubeos/internal/metrics"
	"github.com/serpis172/Yubeos/internal/netx"
	"github.com/serpis172/Yubeos/internal/problem"
)

// Options are the dependencies of the API server.
type Options struct {
	Config  config.Config
	Log     *slog.Logger
	Clock   clock.Clock
	Demo    bool
	UI      fs.FS            // embedded web build; may be empty
	Auth    *auth.Service    // nil: every authenticated route answers 401 (no login configured)
	Net     *netx.Classifier // client IP / origin classification (default: no trusted proxies)
	Metrics *metrics.Hub     // nil: /metrics/* answer 503
	Prefs   PrefsStore       // nil: in-memory (lost on restart)
	Addons  addons.Service   // nil: the add-ons list is empty
	// HostFacts overrides host probing (tests). Default: real probing, cached for factsTTL.
	HostFacts func() hostFacts
}

// Server holds shared state for handlers.
type Server struct {
	opts   Options
	start  time.Time
	cls    *netx.Classifier
	prefs  PrefsStore
	wsOpen atomic.Int32 // live WebSocket connections (capped)

	factsMu  sync.Mutex
	facts    hostFacts
	factsAt  time.Time
	factsSet bool
}

// factsTTL bounds how often the host is probed (exec.LookPath x N, /etc/os-release, kernel) per process.
const factsTTL = 30 * time.Second

// New returns the fully wired http.Handler.
func New(o Options) http.Handler {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Clock == nil {
		o.Clock = clock.Real{}
	}
	if o.Net == nil {
		o.Net = &netx.Classifier{}
	}
	if o.Prefs == nil {
		o.Prefs = &MemPrefs{}
	}
	s := &Server{opts: o, start: o.Clock.Now(), cls: o.Net, prefs: o.Prefs}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.Handle("GET /api/v1/system/info", s.require(RoleUser, s.systemInfo))
	s.registerAuthRoutes(mux)
	s.registerPhase2Routes(mux)
	mux.Handle("GET /api/v1/addons", s.require(RoleUser, s.addonsList))
	// Anything else under /api/ or /ws/ is a JSON 404, never the SPA shell.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { problem.NotFound(w, r) })
	mux.HandleFunc("/ws/", func(w http.ResponseWriter, r *http.Request) { problem.NotFound(w, r) })
	mux.Handle("/", spa(o.UI))

	return chain(mux,
		requestID,
		accessLog(o.Log),
		recoverer(o.Log),
		securityHeaders,
		accessGuard(o.Net),
		hsts(o.Config.Server.Exposure == "remote"),
		hostGuard(o.Config.Server.AllowedHosts),
		limitBody,
		gzipMW,
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
	now := s.opts.Clock.Now()
	writeJSON(w, http.StatusOK, buildSystemInfo(s.opts.Demo, s.cachedFacts(now), now, s.start))
}

// cachedFacts returns host facts, probing at most once per factsTTL.
func (s *Server) cachedFacts(now time.Time) hostFacts {
	s.factsMu.Lock()
	defer s.factsMu.Unlock()
	if s.factsSet && now.Sub(s.factsAt) < factsTTL {
		return s.facts
	}
	probe := s.opts.HostFacts
	if probe == nil {
		probe = func() hostFacts { return readHostFacts(s.opts.Demo) }
	}
	s.facts, s.factsAt, s.factsSet = probe(), now, true
	return s.facts
}
