package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/serpis172/Yubeos/internal/config"
)

func testUI() fstest.MapFS {
	return fstest.MapFS{
		"index.html":      {Data: []byte("<html>shell</html>")},
		"assets/app-1.js": {Data: []byte("console.log(1)")},
	}
}

func newHandler(demo bool, mutate func(*config.Config)) http.Handler {
	cfg := config.Default()
	if mutate != nil {
		mutate(&cfg)
	}
	return New(Options{Config: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Demo: demo, UI: testUI()})
}

func do(h http.Handler, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHealthz(t *testing.T) {
	w := do(newHandler(false, nil), "GET", "/healthz", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("healthz: %d %s", w.Code, w.Body)
	}
	if w.Header().Get("X-Request-Id") == "" {
		t.Fatal("missing X-Request-Id")
	}
}

func TestSecurityHeaders(t *testing.T) {
	w := do(newHandler(false, nil), "GET", "/healthz", nil)
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy", "Cross-Origin-Opener-Policy"} {
		if w.Header().Get(h) == "" {
			t.Errorf("missing header %s", h)
		}
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Error("CSP must forbid framing")
	}
}

func TestSystemInfoIsDefaultDenyOutsideDemo(t *testing.T) {
	w := do(newHandler(false, nil), "GET", "/api/v1/system/info", nil)
	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type = %q", ct)
	}
	var p map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p["type"] != "https://yubeos.dev/problems/unauthenticated" || p["status"].(float64) != 401 {
		t.Fatalf("bad problem body: %v", p)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("API responses must be no-store")
	}
}

func TestSystemInfoInDemo(t *testing.T) {
	w := do(newHandler(true, nil), "GET", "/api/v1/system/info", nil)
	if w.Code != 200 {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	var si SystemInfo
	if err := json.Unmarshal(w.Body.Bytes(), &si); err != nil {
		t.Fatal(err)
	}
	if !si.DemoMode || si.Hostname != "yubeos-demo" || !si.Capabilities["zfs"] {
		t.Fatalf("unexpected info: %+v", si)
	}
}

func TestUnknownAPIPathIsJSON404(t *testing.T) {
	for _, p := range []string{"/api/v1/nope", "/ws/nope"} {
		w := do(newHandler(true, nil), "GET", p, nil)
		if w.Code != 404 || w.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatalf("%s: %d %q", p, w.Code, w.Header().Get("Content-Type"))
		}
	}
}

func TestSPAFallbackAndCaching(t *testing.T) {
	h := newHandler(false, nil)

	w := do(h, "GET", "/disks", nil) // client-side route
	if w.Code != 200 || !strings.Contains(w.Body.String(), "shell") || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("fallback: %d %q cc=%q", w.Code, w.Body, w.Header().Get("Cache-Control"))
	}
	w = do(h, "GET", "/assets/app-1.js", nil)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d cc=%q", w.Code, w.Header().Get("Cache-Control"))
	}
	if w = do(h, "GET", "/assets/missing.js", nil); w.Code != 404 {
		t.Fatalf("missing asset should 404, got %d", w.Code)
	}
	if w = do(h, "POST", "/disks", nil); w.Code != 405 {
		t.Fatalf("POST to SPA should 405, got %d", w.Code)
	}
}

func TestPathTraversalCannotEscapeUI(t *testing.T) {
	w := do(newHandler(false, nil), "GET", "/../../etc/passwd", nil)
	if strings.Contains(w.Body.String(), "root:") {
		t.Fatal("path traversal leaked a file")
	}
}

func TestHostGuard(t *testing.T) {
	h := newHandler(false, func(c *config.Config) { c.Server.AllowedHosts = []string{"nas.lan"} })
	ok := httptest.NewRequest("GET", "/healthz", nil)
	ok.Host = "nas.lan:8443"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, ok)
	if w.Code != 200 {
		t.Fatalf("allowed host rejected: %d", w.Code)
	}
	evil := httptest.NewRequest("GET", "/healthz", nil)
	evil.Host = "evil.example"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, evil)
	if w.Code != 403 {
		t.Fatalf("rebinding host accepted: %d", w.Code)
	}
}

func TestNoUIBuilt(t *testing.T) {
	h := New(Options{Config: config.Default(), Log: slog.New(slog.NewTextHandler(io.Discard, nil)), UI: fstest.MapFS{}})
	w := do(h, "GET", "/", nil)
	if w.Code != 503 || !strings.Contains(w.Body.String(), "make web") {
		t.Fatalf("expected helpful 503, got %d", w.Code)
	}
}

func TestPanicIsRecovered(t *testing.T) {
	h := chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
		requestID, recoverer(slog.New(slog.NewTextHandler(io.Discard, nil))))
	w := do(h, "GET", "/x", nil)
	if w.Code != 500 || strings.Contains(w.Body.String(), "boom") {
		t.Fatalf("panic handling: %d %s", w.Code, w.Body)
	}
}

func TestParseOSRelease(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/os-release"
	data := "# comment\nNAME=\"Ubuntu\"\nVERSION='24.04.1 LTS (Noble Numbat)'\nID=ubuntu\n\nBAD LINE\n"
	if err := writeFile(p, data); err != nil {
		t.Fatal(err)
	}
	got := parseOSRelease(p)
	if got["NAME"] != "Ubuntu" || got["VERSION"] != "24.04.1 LTS (Noble Numbat)" || got["ID"] != "ubuntu" {
		t.Fatalf("parsed: %v", got)
	}
}
