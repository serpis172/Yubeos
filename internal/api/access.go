package api

import (
	"net/http"
	"os"
	"time"

	"github.com/serpis172/Yubeos/internal/problem"
)

type remoteCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"` // ok | warn | fail
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

// remoteStatus is the data behind the "Remote access" card of Settings → Security (docs/REMOTE-ACCESS.md §7).
func (s *Server) remoteStatus(w http.ResponseWriter, r *http.Request) {
	c := s.opts.Config
	remote := c.Server.Exposure == "remote"
	bad := "warn" // not wanted yet: advice only. When exposure=remote the same gaps are failures.
	if remote {
		bad = "fail"
	}
	st := func(ok bool) string {
		if ok {
			return "ok"
		}
		return bad
	}
	_, origin := s.cls.Resolve(r)
	checks := []remoteCheck{
		{"hosts", st(len(c.Server.AllowedHosts) > 0), "allowedHosts is set", "defeats DNS rebinding and Host-header attacks"},
		{"tls", st(c.Server.TLS.Mode != "selfsigned" || c.Server.BehindProxy), "Certificate trusted by browsers", "tls.mode=" + c.Server.TLS.Mode},
		{"auth-native", st(c.Auth.Mode == "native"), "Native credentials", "system passwords are never exposed"},
		{"2fa", st(c.Auth.Require2FA == "all"), "Second factor required for everyone", "auth.require2fa=" + c.Auth.Require2FA},
		{"no-root", st(!c.Auth.AllowRoot), "Root cannot sign in through the web UI", ""},
		{"proxy-trust", st(!c.Server.BehindProxy || len(c.Server.TrustedProxies) > 0), "Forwarded client addresses only from trusted proxies", ""},
	}
	ld := "ok"
	detail := "remote access follows the configuration"
	if s.ld.Active() {
		ld, detail = "warn", "remote requests are currently refused"
	}
	checks = append(checks, remoteCheck{"lockdown", ld, "Lockdown", detail})
	writeJSON(w, http.StatusOK, map[string]any{"exposure": c.Server.Exposure, "lockdown": s.ld.Active(), "currentOrigin": string(origin), "checks": checks})
}

// setLockdown turns the lockdown flag file on or off (the same switch `yubeosctl lockdown` uses).
func (s *Server) setLockdown(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if s.ld == nil || s.ld.path == "" {
		problem.Write(w, r, http.StatusNotImplemented, "not-available", "Lockdown is not available", "")
		return
	}
	var err error
	if req.Enabled {
		err = os.WriteFile(s.ld.path, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644) //nolint:gosec
	} else if err = os.Remove(s.ld.path); os.IsNotExist(err) {
		err = nil
	}
	if err != nil {
		s.opts.Log.Error("lockdown", "err", err)
		problem.Internal(w, r)
		return
	}
	s.ld.mu.Lock()
	s.ld.at = time.Time{} // re-read immediately
	s.ld.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
