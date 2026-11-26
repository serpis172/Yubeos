package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/serpis172/Yubeos/internal/addons"
	"github.com/serpis172/Yubeos/internal/config"
	"github.com/serpis172/Yubeos/internal/metrics"
)

func phase2Handler(t *testing.T, demo bool, mutate func(*config.Config)) http.Handler {
	t.Helper()
	cfg := config.Default()
	if mutate != nil {
		mutate(&cfg)
	}
	hub := metrics.NewHub(metrics.NewDemo(1))
	hub.Tick()
	return New(Options{Config: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Demo: demo, UI: testUI(),
		Metrics: hub, Addons: addons.Demo{}})
}

// send issues a same-origin request with an optional JSON body (demo mode needs no login).
func send(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://nas.lan"+target, strings.NewReader(body))
	r.Header.Set("Origin", "https://nas.lan")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestDemoModeDoesNotPanicOnSessionBoundRoutes(t *testing.T) {
	h := phase2Handler(t, true, nil)
	for _, c := range []struct{ method, path, body string }{
		{"PUT", "/api/v1/auth/password", `{"currentPassword":"a","newPassword":"correct horse battery staple"}`},
		{"DELETE", "/api/v1/auth/devices/abc", ""},
		{"POST", "/api/v1/auth/devices:revokeAll", ""},
	} {
		if w := send(h, c.method, c.path, c.body); w.Code != http.StatusNoContent {
			t.Errorf("%s %s = %d %s, want 204", c.method, c.path, w.Code, w.Body)
		}
	}
}

func TestMetricsEndpoints(t *testing.T) {
	h := phase2Handler(t, true, nil)
	var cat metrics.Catalog
	w := send(h, "GET", "/api/v1/metrics/catalog", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &cat) != nil || len(cat.Series) == 0 || len(cat.Devices) == 0 {
		t.Fatalf("catalog: %d %s", w.Code, w.Body)
	}
	var snap metrics.Snapshot
	w = send(h, "GET", "/api/v1/metrics/snapshot", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &snap) != nil || snap.Values["cpu.0.util"] == 0 && snap.T == 0 {
		t.Fatalf("snapshot: %d %s", w.Code, w.Body)
	}
	var hist metrics.History
	w = send(h, "GET", "/api/v1/metrics/history?series=cpu.*,mem.used&range=5m", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &hist) != nil || len(hist.Series["mem.used"].V) == 0 {
		t.Fatalf("history: %d %s", w.Code, w.Body)
	}
	for _, bad := range []string{"?range=5m", "?series=cpu.*&range=2y", "?series=cpu.*&range=5m&step=x"} {
		if w := send(h, "GET", "/api/v1/metrics/history"+bad, ""); w.Code != 400 {
			t.Errorf("history%s = %d, want 400", bad, w.Code)
		}
	}
}

func TestMetricsUnavailableWithoutHub(t *testing.T) {
	h := newHandler(true, nil)
	if w := do(h, "GET", "/api/v1/metrics/snapshot", nil); w.Code != 503 {
		t.Fatalf("got %d", w.Code)
	}
}

func TestPreferencesRoundTripAndValidation(t *testing.T) {
	h := phase2Handler(t, true, nil)
	var p preferences
	if w := send(h, "GET", "/api/v1/me/preferences", ""); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Theme != "system" || p.UpdatedAt != 0 {
		t.Fatalf("defaults: %d %s", w.Code, w.Body)
	}
	body := `{"theme":"custom:mine","language":"it","units":"bits","temperature":"f","customThemes":[{"id":"mine"}],"updatedAt":0}`
	if w := send(h, "PUT", "/api/v1/me/preferences", body); w.Code != 200 {
		t.Fatalf("put: %d %s", w.Code, w.Body)
	}
	w := send(h, "GET", "/api/v1/me/preferences", "")
	if json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Language != "it" || p.Units != "bits" || p.UpdatedAt == 0 || len(p.CustomThemes) != 1 {
		t.Fatalf("saved prefs: %s", w.Body)
	}
	for _, bad := range []string{
		`{"theme":"../x","language":"en","units":"bytes","temperature":"c"}`,
		`{"theme":"system","language":"xx","units":"bytes","temperature":"c"}`,
		`{"theme":"system","language":"en","units":"nibbles","temperature":"c"}`,
	} {
		if w := send(h, "PUT", "/api/v1/me/preferences", bad); w.Code != 422 {
			t.Errorf("%s = %d, want 422", bad, w.Code)
		}
	}
	if w := send(h, "PUT", "/api/v1/me/preferences", `{"theme":"system","pad":"`+strings.Repeat("x", maxPrefsBytes)+`"}`); w.Code != 413 {
		t.Errorf("oversized body = %d, want 413", w.Code)
	}
}

func TestUnimplementedAuthEndpointsAnswer501(t *testing.T) {
	h := phase2Handler(t, true, nil)
	for _, p := range []string{"POST /api/v1/auth/webauthn/register/begin", "POST /api/v1/auth/webauthn/login/begin",
		"POST /api/v1/security/lockdown"} {
		m, path, _ := strings.Cut(p, " ")
		if w := send(h, m, path, "{}"); w.Code != http.StatusNotImplemented {
			t.Errorf("%s = %d, want 501", p, w.Code)
		}
	}
	// authenticated routes stay behind the login: 401, never 501, without a session
	if w := send(newHandler(false, nil), "POST", "/api/v1/auth/webauthn/register/begin", "{}"); w.Code != 401 {
		t.Errorf("unauthenticated = %d, want 401", w.Code)
	}
}

func TestDemoSecondFactorHelpers(t *testing.T) {
	h := phase2Handler(t, true, nil)
	for _, c := range []struct{ m, p, body string }{
		{"POST", "/api/v1/auth/totp/enroll", ""}, {"POST", "/api/v1/auth/totp/confirm", `{"code":"123456"}`},
		{"POST", "/api/v1/auth/recovery-codes:regenerate", ""}, {"POST", "/api/v1/auth/stepup/begin", ""},
	} {
		if w := send(h, c.m, c.p, c.body); w.Code != 200 {
			t.Errorf("%s %s = %d %s", c.m, c.p, w.Code, w.Body)
		}
	}
	if w := send(h, "POST", "/api/v1/auth/stepup", `{"challengeId":"demo","method":"totp","code":"123456"}`); w.Code != 204 {
		t.Errorf("stepup = %d", w.Code)
	}
}

func TestFactorsAndRemoteStatus(t *testing.T) {
	h := phase2Handler(t, true, func(c *config.Config) { c.Auth.Require2FA = "all" })
	var f struct {
		Required bool  `json:"required"`
		Passkeys []any `json:"passkeys"`
	}
	if w := send(h, "GET", "/api/v1/auth/factors", ""); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &f) != nil || !f.Required || f.Passkeys == nil {
		t.Fatalf("factors: %d %s", w.Code, w.Body)
	}
	var rs struct {
		Exposure string        `json:"exposure"`
		Checks   []remoteCheck `json:"checks"`
	}
	if w := send(h, "GET", "/api/v1/security/remote-status", ""); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &rs) != nil || rs.Exposure != "lan" || len(rs.Checks) != 3 {
		t.Fatalf("remote-status: %d %s", w.Code, w.Body)
	}
	cfg := config.Default()
	cfg.Auth.Require2FA = "none"
	got := buildRemoteStatus(cfg)["checks"].([]remoteCheck)
	if got[1].ID != "twofactor" || got[1].Status != "fail" {
		t.Fatalf("require2fa none must fail the twofactor check: %+v", got)
	}
}

func TestAddonsList(t *testing.T) {
	h := phase2Handler(t, true, nil)
	var out addonList
	w := send(h, "GET", "/api/v1/addons", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Items) != 3 {
		t.Fatalf("addons: %d %s", w.Code, w.Body)
	}
	if out.Items[0].URL != "http://192.168.1.10:20001/" || out.Items[0].URLBlocked != "" {
		t.Fatalf("direct link: %+v", out.Items[0])
	}
	h = phase2Handler(t, true, func(c *config.Config) { c.Addons.BaseDomain = "apps.nas.example.org" })
	_ = json.Unmarshal(send(h, "GET", "/api/v1/addons", "").Body.Bytes(), &out)
	if out.Items[0].URL != "https://jellyfin.apps.nas.example.org/" {
		t.Fatalf("isolated origin: %+v", out.Items[0])
	}
	// nothing configured: an empty list, never an error
	if w := send(newHandler(true, nil), "GET", "/api/v1/addons", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("empty: %d %s", w.Code, w.Body)
	}
}
