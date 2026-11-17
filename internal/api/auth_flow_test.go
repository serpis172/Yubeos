package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/serpis172/Yubeos/internal/audit"
	"github.com/serpis172/Yubeos/internal/auth"
	"github.com/serpis172/Yubeos/internal/clock"
	"github.com/serpis172/Yubeos/internal/config"
	"github.com/serpis172/Yubeos/internal/db"
	"github.com/serpis172/Yubeos/internal/identity"
	"github.com/serpis172/Yubeos/internal/netx"
	"github.com/serpis172/Yubeos/internal/priv"
	"github.com/serpis172/Yubeos/internal/privd"
	"github.com/serpis172/Yubeos/internal/privd/credstore"
)

const flowPW = "correct-horse-battery-staple"

type flow struct {
	t    *testing.T
	h    http.Handler
	root priv.Caller
	clk  *clock.Fake
	d    *db.DB
}

func newFlow(t *testing.T, require2fa string, mutate func(*config.Config, *netx.Classifier)) *flow {
	t.Helper()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	clk := clock.NewFake(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "y.db"), db.Migrations(), quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	fast := credstore.Params{M: 1024, T: 1, P: 1}
	st, err := credstore.Open(context.Background(), t.TempDir(), credstore.Options{Params: &fast, Clock: clk, Log: quiet})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := privd.NewServer(quiet)
	credstore.Register(srv, st)

	cfg := config.Default()
	cfg.Auth.Require2FA = require2fa
	cls := &netx.Classifier{HasOverlay: func() bool { return false }}
	if mutate != nil {
		mutate(&cfg, cls)
	}
	svc := auth.NewService(auth.Deps{
		Config: auth.Config{Require2FA: cfg.Auth.Require2FA}, Priv: privd.Direct{Server: srv, UID: 998},
		Identity: identity.Static{
			"alice": {Username: "alice", UID: 1000, Role: identity.Admin},
			"bob":   {Username: "bob", UID: 1001, Role: identity.User},
		},
		Sessions: auth.NewSessions(d, clk, auth.SessionConfig{IdleLocal: 30 * time.Minute, IdleRemote: 15 * time.Minute, MaxAge: 12 * time.Hour}),
		Throttle: auth.NewThrottle(d, clk, auth.ThrottleConfig{}),
		Audit:    audit.New(d, clk, quiet), Clock: clk, Log: quiet,
	})
	h := New(Options{Config: cfg, Log: quiet, Clock: clk, UI: testUI(), Auth: svc, Net: cls})
	return &flow{t: t, h: h, root: privd.Direct{Server: srv, UID: 0}, clk: clk, d: d}
}

type reqOpt func(*http.Request)

func fromIP(ip string) reqOpt { return func(r *http.Request) { r.RemoteAddr = ip + ":40000" } }
func withCookie(c string) reqOpt {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: sessionCookie, Value: c}) }
}
func withCSRF(v string) reqOpt      { return func(r *http.Request) { r.Header.Set("X-Yubeos-CSRF", v) } }
func withHeader(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }
func plainHTTP() reqOpt             { return func(r *http.Request) { r.TLS = nil } }

func (f *flow) do(method, path string, body any, opts ...reqOpt) *httptest.ResponseRecorder {
	f.t.Helper()
	var rd io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			rd = strings.NewReader(b)
		default:
			raw, _ := json.Marshal(b)
			rd = bytes.NewReader(raw)
		}
	}
	r := httptest.NewRequest(method, path, rd)
	r.Host = "nas.lan"
	r.RemoteAddr = "192.168.1.20:40000"
	r.TLS = &tls.ConnectionState{}
	r.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Chrome/130.0 Safari/537.36")
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	for _, o := range opts {
		o(r)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func (f *flow) enrol(user, pw string) {
	f.t.Helper()
	var tok struct{ Token string }
	if err := f.root.Call(context.Background(), "auth.setup.create", priv.Meta{}, map[string]any{"username": user, "purpose": "first-password"}, &tok); err != nil {
		f.t.Fatal(err)
	}
	if w := f.do("POST", "/api/v1/auth/setup", map[string]string{"token": tok.Token, "newPassword": pw}); w.Code != 204 {
		f.t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
}

func cookieOf(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	return nil
}

func problemType(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("not JSON: %s", w.Body)
	}
	s, _ := p["type"].(string)
	return strings.TrimPrefix(s, "https://yubeos.dev/problems/")
}

type me struct {
	Username           string   `json:"username"`
	Role               string   `json:"role"`
	CSRF               string   `json:"csrf"`
	Factors            []string `json:"factors"`
	RemoteSession      bool     `json:"remoteSession"`
	EnrollmentRequired bool     `json:"enrollmentRequired"`
}

func (f *flow) login(user, pw string, opts ...reqOpt) (*httptest.ResponseRecorder, me) {
	f.t.Helper()
	w := f.do("POST", "/api/v1/auth/login", map[string]string{"username": user, "password": pw}, opts...)
	var m me
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w, m
}

func TestLoginCookieAttributesAndMe(t *testing.T) {
	f := newFlow(t, "none", nil)
	f.enrol("bob", flowPW)
	w, m := f.login("bob", flowPW)
	if w.Code != 200 || m.Username != "bob" || m.Role != "user" || m.CSRF == "" {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	c := cookieOf(w)
	if c == nil || !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Domain != "" || c.MaxAge <= 0 {
		t.Fatalf("session cookie must be __Host- compliant (Secure, HttpOnly, SameSite=Strict, Path=/, no Domain): %+v", c)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("auth responses must not be cached")
	}
	got := f.do("GET", "/api/v1/auth/me", nil, withCookie(c.Value))
	var m2 me
	_ = json.Unmarshal(got.Body.Bytes(), &m2)
	if got.Code != 200 || m2.CSRF != m.CSRF || m2.Username != "bob" || len(m2.Factors) != 1 {
		t.Fatalf("me: %d %s", got.Code, got.Body)
	}
	if w := f.do("GET", "/api/v1/system/info", nil, withCookie(c.Value)); w.Code != 200 {
		t.Fatalf("system info with session: %d", w.Code)
	}
	if w := f.do("GET", "/api/v1/system/info", nil); w.Code != 401 {
		t.Fatalf("without cookie must be 401, got %d", w.Code)
	}
}

func TestCSRFAndOriginProtection(t *testing.T) {
	f := newFlow(t, "none", nil)
	f.enrol("bob", flowPW)
	w, m := f.login("bob", flowPW)
	ck := cookieOf(w).Value

	// state-changing request with a valid cookie but no CSRF header
	if w := f.do("POST", "/api/v1/auth/logout", nil, withCookie(ck)); w.Code != 403 || problemType(t, w) != "csrf" {
		t.Fatalf("missing CSRF header: %d %s", w.Code, w.Body)
	}
	// wrong CSRF value
	if w := f.do("POST", "/api/v1/auth/logout", nil, withCookie(ck), withCSRF("nope")); w.Code != 403 {
		t.Fatalf("wrong CSRF: %d", w.Code)
	}
	// right CSRF but foreign Origin / cross-site fetch metadata / "null" origin
	for _, hdr := range [][2]string{{"Origin", "https://evil.example"}, {"Origin", "null"}, {"Sec-Fetch-Site", "cross-site"}, {"Sec-Fetch-Site", "same-site"}} {
		if w := f.do("POST", "/api/v1/auth/logout", nil, withCookie(ck), withCSRF(m.CSRF), withHeader(hdr[0], hdr[1])); w.Code != 403 {
			t.Errorf("%s=%s must be refused, got %d", hdr[0], hdr[1], w.Code)
		}
	}
	// the session survived all of that; a same-origin request with the token succeeds and clears the cookie
	w = f.do("POST", "/api/v1/auth/logout", nil, withCookie(ck), withCSRF(m.CSRF), withHeader("Origin", "https://nas.lan"))
	if w.Code != 204 {
		t.Fatalf("legitimate logout: %d %s", w.Code, w.Body)
	}
	if c := cookieOf(w); c == nil || c.MaxAge >= 0 {
		t.Fatalf("logout must clear the cookie: %+v", c)
	}
	if w := f.do("GET", "/api/v1/auth/me", nil, withCookie(ck)); w.Code != 401 {
		t.Fatalf("revoked session must not work: %d", w.Code)
	}
}

func TestLoginRequiresTLSAndSameOrigin(t *testing.T) {
	f := newFlow(t, "none", nil)
	f.enrol("bob", flowPW)
	if w, _ := f.login("bob", flowPW, plainHTTP()); w.Code != 400 || problemType(t, w) != "tls-required" {
		t.Fatalf("plain HTTP login: %d %s", w.Code, w.Body)
	}
	if w, _ := f.login("bob", flowPW, withHeader("Origin", "https://evil.example")); w.Code != 403 {
		t.Fatalf("login CSRF (foreign origin) must be refused: %d", w.Code)
	}
	// behind a trusted TLS-terminating proxy, X-Forwarded-Proto is honoured — but only from the proxy
	g := newFlow(t, "none", func(c *config.Config, cl *netx.Classifier) {
		c.Server.BehindProxy = true
		c.Server.TrustedProxies = []string{"127.0.0.1"}
		cl.Trusted, _ = netx.ParseNets([]string{"127.0.0.1"})
	})
	g.enrol("bob", flowPW)
	if w, _ := g.login("bob", flowPW, plainHTTP(), fromIP("127.0.0.1"), withHeader("X-Forwarded-Proto", "https"), withHeader("X-Forwarded-For", "192.168.1.5")); w.Code != 200 {
		t.Fatalf("trusted proxy with https: %d %s", w.Code, w.Body)
	}
	if w, _ := g.login("bob", flowPW, plainHTTP(), fromIP("192.168.1.99"), withHeader("X-Forwarded-Proto", "https")); w.Code != 400 {
		t.Fatalf("X-Forwarded-Proto from a non-proxy must be ignored: %d", w.Code)
	}
}

func TestFailedLoginsAreUniformOnTheWire(t *testing.T) {
	f := newFlow(t, "none", nil)
	f.enrol("bob", flowPW)
	var bodies []string
	for _, tc := range [][2]string{{"bob", "wrong-password-xyz"}, {"ghost", flowPW}, {"alice", flowPW}} { // alice has no credentials
		w, _ := f.login(tc[0], tc[1])
		if w.Code != 401 || problemType(t, w) != "invalid-credentials" {
			t.Fatalf("%v: %d %s", tc, w.Code, w.Body)
		}
		var p map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &p)
		delete(p, "requestId")
		delete(p, "instance")
		b, _ := json.Marshal(p)
		bodies = append(bodies, string(b))
	}
	if bodies[0] != bodies[1] || bodies[1] != bodies[2] {
		t.Fatalf("failure bodies differ (account enumeration): %v", bodies)
	}
}

func TestLockoutOverHTTP(t *testing.T) {
	f := newFlow(t, "none", nil)
	f.enrol("bob", flowPW)
	for i := 0; i < 10; i++ {
		f.login("bob", "wrong-password-"+string(rune('a'+i)))
	}
	w, _ := f.login("bob", flowPW)
	if w.Code != 429 || problemType(t, w) != "locked-out" || w.Header().Get("Retry-After") == "" {
		t.Fatalf("lockout: %d %s retry=%q", w.Code, w.Body, w.Header().Get("Retry-After"))
	}
}

func TestRemoteOriginPolicyOverHTTP(t *testing.T) {
	f := newFlow(t, "none", nil)
	f.enrol("bob", flowPW)
	if w, _ := f.login("bob", flowPW, fromIP("203.0.113.50")); w.Code != 403 || problemType(t, w) != "enrollment-required-local" {
		t.Fatalf("remote login without 2FA: %d %s", w.Code, w.Body)
	}
	var tok struct{ Token string }
	_ = f.root.Call(context.Background(), "auth.setup.create", priv.Meta{}, map[string]any{"username": "alice", "purpose": "first-password"}, &tok)
	if w := f.do("POST", "/api/v1/auth/setup", map[string]string{"token": tok.Token, "newPassword": flowPW}, fromIP("203.0.113.50")); w.Code != 403 || problemType(t, w) != "local-origin-required" {
		t.Fatalf("remote setup: %d %s", w.Code, w.Body)
	}
	// spoofed proxy headers from an untrusted peer do not make a remote client local
	if w, _ := f.login("bob", flowPW, fromIP("203.0.113.50"), withHeader("X-Forwarded-For", "192.168.1.5")); w.Code != 403 {
		t.Fatalf("spoofed X-Forwarded-For must not grant local origin: %d", w.Code)
	}
}

func TestAccessAllowListBlocksRemoteBeforeAnything(t *testing.T) {
	f := newFlow(t, "none", func(c *config.Config, cl *netx.Classifier) {
		cl.Allow, _ = netx.ParseNets([]string{"198.51.100.0/24"})
	})
	if w := f.do("GET", "/healthz", nil, fromIP("203.0.113.9")); w.Code != 403 || problemType(t, w) != "access-denied" {
		t.Fatalf("remote outside the allow-list: %d %s", w.Code, w.Body)
	}
	if w := f.do("GET", "/healthz", nil, fromIP("198.51.100.7")); w.Code != 200 {
		t.Fatalf("allow-listed remote: %d", w.Code)
	}
	if w := f.do("GET", "/healthz", nil, fromIP("192.168.1.2")); w.Code != 200 {
		t.Fatalf("local always passes: %d", w.Code)
	}
}

func TestEnrollmentOnlySessionIsConfined(t *testing.T) {
	f := newFlow(t, "admins", nil)
	f.enrol("alice", flowPW)
	w, m := f.login("alice", flowPW)
	if w.Code != 200 || !m.EnrollmentRequired {
		t.Fatalf("admin on a local network without 2FA must get an enrollment-only session: %d %s", w.Code, w.Body)
	}
	ck := cookieOf(w).Value
	if w := f.do("GET", "/api/v1/system/info", nil, withCookie(ck)); w.Code != 403 || problemType(t, w) != "enrollment-required" {
		t.Fatalf("enrollment-only session must be blocked from normal routes: %d %s", w.Code, w.Body)
	}
	if w := f.do("GET", "/api/v1/auth/me", nil, withCookie(ck)); w.Code != 200 {
		t.Fatalf("/auth/me must stay reachable: %d", w.Code)
	}
}

func TestSetupLinkIsSingleUseOverHTTP(t *testing.T) {
	f := newFlow(t, "none", nil)
	var tok struct{ Token string }
	_ = f.root.Call(context.Background(), "auth.setup.create", priv.Meta{}, map[string]any{"username": "bob", "purpose": "first-password"}, &tok)
	if w := f.do("POST", "/api/v1/auth/setup", map[string]string{"token": tok.Token, "newPassword": "short"}); w.Code != 422 {
		t.Fatalf("weak password: %d %s", w.Code, w.Body)
	} else if !strings.Contains(w.Body.String(), "/newPassword") {
		t.Fatalf("validation error should point at the field: %s", w.Body)
	}
	if w := f.do("POST", "/api/v1/auth/setup", map[string]string{"token": tok.Token, "newPassword": flowPW}); w.Code != 204 {
		t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	if w := f.do("POST", "/api/v1/auth/setup", map[string]string{"token": tok.Token, "newPassword": flowPW + "2"}); w.Code != 400 || problemType(t, w) != "invalid-setup-link" {
		t.Fatalf("reuse must fail: %d %s", w.Code, w.Body)
	}
}

func TestPasswordChangeOverHTTP(t *testing.T) {
	f := newFlow(t, "none", nil)
	f.enrol("bob", flowPW)
	w, m := f.login("bob", flowPW)
	ck := cookieOf(w).Value
	hdr := []reqOpt{withCookie(ck), withCSRF(m.CSRF)}
	if w := f.do("PUT", "/api/v1/auth/password", map[string]string{"currentPassword": "not-it-at-all-1", "newPassword": "a-brand-new-password"}, hdr...); w.Code != 403 || problemType(t, w) != "wrong-password" {
		t.Fatalf("wrong current: %d %s", w.Code, w.Body)
	}
	if w := f.do("PUT", "/api/v1/auth/password", map[string]string{"currentPassword": flowPW, "newPassword": "weak"}, hdr...); w.Code != 422 {
		t.Fatalf("weak new: %d %s", w.Code, w.Body)
	}
	if w := f.do("PUT", "/api/v1/auth/password", map[string]string{"currentPassword": flowPW, "newPassword": "a-brand-new-password"}, hdr...); w.Code != 204 {
		t.Fatalf("change: %d %s", w.Code, w.Body)
	}
	if w, _ := f.login("bob", flowPW); w.Code != 401 {
		t.Fatal("the old password must stop working")
	}
	if w, _ := f.login("bob", "a-brand-new-password"); w.Code != 200 {
		t.Fatal("the new password must work")
	}
}

func TestDevicesListAndRevocationAreScopedToTheCaller(t *testing.T) {
	f := newFlow(t, "none", nil)
	f.enrol("bob", flowPW)
	f.enrol("alice", flowPW)
	w1, m1 := f.login("bob", flowPW)
	_, _ = f.login("bob", flowPW, withHeader("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) Safari/604.1"))
	wa, _ := f.login("alice", flowPW)
	c1 := cookieOf(w1).Value

	lst := f.do("GET", "/api/v1/auth/devices", nil, withCookie(c1))
	var out struct{ Items []deviceDTO }
	_ = json.Unmarshal(lst.Body.Bytes(), &out)
	if lst.Code != 200 || len(out.Items) != 2 {
		t.Fatalf("list: %d %s", lst.Code, lst.Body)
	}
	cur, other := 0, ""
	for _, d := range out.Items {
		if d.Current {
			cur++
		} else {
			other = d.ID
			if d.OS != "iOS" || d.Browser != "Safari" {
				t.Errorf("UA summary: %+v", d)
			}
		}
	}
	if cur != 1 || other == "" {
		t.Fatalf("exactly one current session expected: %+v", out.Items)
	}
	// alice cannot revoke bob's session by guessing its id
	var am me
	_ = json.Unmarshal(f.do("GET", "/api/v1/auth/me", nil, withCookie(cookieOf(wa).Value)).Body.Bytes(), &am)
	if w := f.do("DELETE", "/api/v1/auth/devices/"+other, nil, withCookie(cookieOf(wa).Value), withCSRF(am.CSRF)); w.Code != 404 {
		t.Fatalf("cross-user revoke must be 404, got %d", w.Code)
	}
	if w := f.do("DELETE", "/api/v1/auth/devices/"+other, nil, withCookie(c1), withCSRF(m1.CSRF)); w.Code != 204 {
		t.Fatalf("own revoke: %d", w.Code)
	}
	if w := f.do("POST", "/api/v1/auth/devices:revokeAll", nil, withCookie(c1), withCSRF(m1.CSRF)); w.Code != 204 {
		t.Fatalf("revoke all: %d", w.Code)
	}
	if w := f.do("GET", "/api/v1/auth/me", nil, withCookie(c1)); w.Code != 401 {
		t.Fatal("revokeAll must also end the current session")
	}
}

func TestLoginBodyValidation(t *testing.T) {
	f := newFlow(t, "none", nil)
	if w := f.do("POST", "/api/v1/auth/login", strings.Repeat("x", 5000)); w.Code != 413 {
		t.Fatalf("oversized login body: %d", w.Code)
	}
	for _, body := range []string{`{`, `[]`, `{"username":"a","password":"b","admin":true}`, `{"username":"","password":""}`} {
		if w := f.do("POST", "/api/v1/auth/login", body); w.Code != 400 && w.Code != 422 {
			t.Errorf("%s: %d", body, w.Code)
		}
	}
}

func TestHelperOutageIsA503NotA500(t *testing.T) {
	f := newFlow(t, "none", nil)
	// replace the helper by a dead socket
	svcField := f.h
	_ = svcField
	g := newFlowWithDeadHelper(t)
	if w, _ := g.login("bob", flowPW); w.Code != 503 || problemType(t, w) != "priv-unavailable" {
		t.Fatalf("helper down: %d %s", w.Code, w.Body)
	}
}

func newFlowWithDeadHelper(t *testing.T) *flow {
	t.Helper()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	clk := clock.NewFake(time.Now())
	d, _ := db.Open(context.Background(), filepath.Join(t.TempDir(), "y.db"), db.Migrations(), quiet)
	t.Cleanup(func() { d.Close() })
	svc := auth.NewService(auth.Deps{Config: auth.Config{Require2FA: "none"}, Priv: priv.NewClient(filepath.Join(t.TempDir(), "none.sock")),
		Identity: identity.Static{"bob": {Username: "bob", UID: 1, Role: identity.User}},
		Sessions: auth.NewSessions(d, clk, auth.SessionConfig{IdleLocal: time.Hour, MaxAge: time.Hour}),
		Throttle: auth.NewThrottle(d, clk, auth.ThrottleConfig{}), Audit: audit.New(d, clk, quiet), Clock: clk, Log: quiet})
	return &flow{t: t, h: New(Options{Config: config.Default(), Log: quiet, Clock: clk, UI: testUI(), Auth: svc})}
}
