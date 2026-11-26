package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/serpis172/Yubeos/internal/config"
	"github.com/serpis172/Yubeos/internal/metrics"
	"github.com/serpis172/Yubeos/internal/problem"
)

// registerPhase2Routes wires what the web UI already calls: dashboard metrics, preferences, the security page and
// the second-factor endpoints. TOTP, recovery codes and step-up are implemented; passkeys and lockdown answer a
// clean 501 instead of a 404.
func (s *Server) registerPhase2Routes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/metrics/catalog", s.require(RoleUser, s.metricsCatalog))
	mux.Handle("GET /api/v1/metrics/snapshot", s.require(RoleUser, s.metricsSnapshot))
	mux.Handle("GET /api/v1/metrics/history", s.require(RoleUser, s.metricsHistory))

	mux.Handle("GET /ws/metrics", s.require(RoleUser, s.wsMetrics))

	mux.Handle("GET /api/v1/me/preferences", s.require(RoleUser, s.getPrefs))
	mux.Handle("PUT /api/v1/me/preferences", s.require(RoleUser, s.putPrefs))

	mux.Handle("GET /api/v1/auth/factors", s.require(RoleUser, s.factors, allowEnrollment()))
	mux.Handle("GET /api/v1/security/remote-status", s.require(RoleAdmin, s.remoteStatus))
	mux.Handle("POST /api/v1/security/lockdown", s.require(RoleAdmin, s.notImplemented("Lockdown")))

	// Second factors: TOTP, recovery codes and step-up (internal/api/secondfactor.go).
	mux.HandleFunc("POST /api/v1/auth/2fa/verify", s.verify2FA)
	mux.Handle("POST /api/v1/auth/totp/enroll", s.require(RoleUser, s.totpEnroll, allowEnrollment()))
	mux.Handle("POST /api/v1/auth/totp/confirm", s.require(RoleUser, s.totpConfirm, allowEnrollment()))
	mux.Handle("DELETE /api/v1/auth/totp", s.require(RoleUser, s.totpRemove))
	mux.Handle("POST /api/v1/auth/recovery-codes:regenerate", s.require(RoleUser, s.recoveryRegenerate))
	mux.Handle("POST /api/v1/auth/stepup/begin", s.require(RoleUser, s.stepUpBegin))
	mux.Handle("POST /api/v1/auth/stepup", s.require(RoleUser, s.stepUp))

	// Passkeys (WebAuthn) are not implemented yet: a clean 501 instead of a 404.
	for _, p := range []string{"POST /api/v1/auth/webauthn/login/begin", "POST /api/v1/auth/webauthn/login/finish"} {
		mux.HandleFunc(p, s.notImplemented("Passkey sign-in"))
	}
	for _, p := range []string{"POST /api/v1/auth/webauthn/register/begin", "POST /api/v1/auth/webauthn/register/finish", "DELETE /api/v1/auth/webauthn/{id}"} {
		mux.Handle(p, s.require(RoleUser, s.notImplemented("Passkeys"), allowEnrollment()))
	}
}

func (s *Server) notImplemented(what string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, http.StatusNotImplemented, "not-implemented", what+" is not available yet",
			"This part of authentication is planned for a later phase.")
	}
}

// ---- metrics

func (s *Server) hub(w http.ResponseWriter, r *http.Request) *metrics.Hub {
	if s.opts.Metrics == nil {
		problem.Write(w, r, http.StatusServiceUnavailable, "unavailable", "Metrics are not running", "")
	}
	return s.opts.Metrics
}

func (s *Server) metricsCatalog(w http.ResponseWriter, r *http.Request) {
	if h := s.hub(w, r); h != nil {
		writeJSON(w, http.StatusOK, h.Catalog())
	}
}

func (s *Server) metricsSnapshot(w http.ResponseWriter, r *http.Request) {
	if h := s.hub(w, r); h != nil {
		writeJSON(w, http.StatusOK, h.Snapshot())
	}
}

func (s *Server) metricsHistory(w http.ResponseWriter, r *http.Request) {
	h := s.hub(w, r)
	if h == nil {
		return
	}
	q := r.URL.Query()
	step := 0
	if v := q.Get("step"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			problem.Write(w, r, http.StatusBadRequest, "bad-request", "Invalid step", "step must be a whole number of seconds")
			return
		}
		step = n
	}
	var patterns []string
	for _, p := range strings.Split(q.Get("series"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			patterns = append(patterns, p)
		}
	}
	res, err := h.History(r.Context(), patterns, q.Get("range"), step)
	var bad metrics.ErrInvalid
	if errors.As(err, &bad) {
		problem.Write(w, r, http.StatusBadRequest, "bad-request", "Invalid history query", bad.Error())
		return
	}
	if err != nil {
		s.opts.Log.Error("metrics history", "err", err)
		problem.Write(w, r, http.StatusInternalServerError, "internal", "Could not read history", "")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// wsMetrics pushes {"t":ms,"v":{key:value}} once per second (docs/modules/dashboard.md). Optional ?series=a,b.*
// narrows the keys. The socket closes when the session ends or the client goes away.
func (s *Server) wsMetrics(w http.ResponseWriter, r *http.Request) {
	h := s.hub(w, r)
	if h == nil {
		return
	}
	var patterns []string
	for _, p := range strings.Split(r.URL.Query().Get("series"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			patterns = append(patterns, p)
		}
	}
	if len(patterns) > 64 {
		problem.Write(w, r, http.StatusBadRequest, "bad-request", "Too many series", "at most 64 keys or patterns")
		return
	}
	if s.wsOpen.Add(1) > wsMaxConns {
		s.wsOpen.Add(-1)
		problem.Write(w, r, http.StatusServiceUnavailable, "unavailable", "Too many live connections", "")
		return
	}
	defer s.wsOpen.Add(-1)
	ws, ok := wsUpgrade(w, r, s.sameOrigin)
	if !ok {
		return
	}
	defer ws.Close()

	gone := make(chan struct{})
	go func() { _ = ws.readLoop(); close(gone) }()

	var token string
	if c, err := r.Cookie(sessionCookie); err == nil && !s.opts.Demo && s.opts.Auth != nil {
		token = c.Value
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	ping := time.NewTicker(wsPingEvery)
	defer ping.Stop()
	recheck := time.NewTicker(30 * time.Second)
	defer recheck.Stop()
	var last int64
	for {
		select {
		case <-gone:
			return
		case <-r.Context().Done():
			return
		case <-ping.C:
			if ws.write(opPing, nil) != nil {
				return
			}
		case <-recheck.C: // a revoked/expired session must not keep receiving data
			if token != "" {
				if _, err := s.opts.Auth.Authenticate(r.Context(), token); err != nil {
					ws.closeWith(1008)
					return
				}
			}
		case <-tick.C:
			snap := h.Snapshot()
			if snap.T == 0 || snap.T == last {
				continue
			}
			last = snap.T
			vals := snap.Values
			if len(patterns) > 0 {
				vals = make(map[string]float64, len(snap.Values))
				for k, v := range snap.Values {
					if metrics.Matches(patterns, k) {
						vals[k] = v
					}
				}
			}
			b, _ := json.Marshal(map[string]any{"t": snap.T, "v": vals})
			if ws.write(opText, b) != nil {
				return
			}
		}
	}
}

// ---- security page

type remoteCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"` // ok | warn | fail
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

func (s *Server) remoteStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, buildRemoteStatus(s.opts.Config))
}

func buildRemoteStatus(c config.Config) map[string]any {
	exposure := "lan"
	if c.Server.BehindProxy {
		exposure = "remote"
	}
	tls := remoteCheck{ID: "tls", Status: "ok", Title: "Certificate trusted by browsers"}
	if !c.Server.BehindProxy && c.Server.TLS.Mode == "selfsigned" {
		tls.Status, tls.Detail = "warn", "A self-signed certificate makes browsers show a warning. Use a certificate file, ACME or a tunnel."
	}
	twofa := remoteCheck{ID: "twofactor", Status: "ok", Title: "Second factor required"}
	switch c.Auth.Require2FA {
	case "none":
		twofa.Status, twofa.Detail = "fail", "auth.require2fa is none: remote logins would rely on the password alone."
	case "admins":
		twofa.Status, twofa.Detail = "warn", "Only administrators need a second factor."
	}
	hosts := remoteCheck{ID: "hosts", Status: "ok", Title: "Allowed host names are listed"}
	if len(c.Server.AllowedHosts) == 0 {
		hosts.Status, hosts.Detail = "warn", "server.allowedHosts is empty: any Host header is accepted."
	}
	return map[string]any{"exposure": exposure, "lockdown": false, "checks": []remoteCheck{tls, twofa, hosts}}
}
