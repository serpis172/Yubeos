package api

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/serpis172/Yubeos/internal/clock"
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

func TestCSPIsStrict(t *testing.T) {
	csp := do(newHandler(false, nil), "GET", "/healthz", nil).Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "wss:") {
		t.Error("connect-src must not allow arbitrary wss: hosts")
	}
	for _, want := range []string{"connect-src 'self'", "object-src 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q: %s", want, csp)
		}
	}
}

func TestHSTSOnlyInRemoteMode(t *testing.T) {
	if h := do(newHandler(false, nil), "GET", "/healthz", nil).Header().Get("Strict-Transport-Security"); h != "" {
		t.Fatalf("HSTS must not be sent in lan mode, got %q", h)
	}
	h := newHandler(false, func(c *config.Config) { c.Server.Exposure = "remote" })
	if v := do(h, "GET", "/healthz", nil).Header().Get("Strict-Transport-Security"); !strings.Contains(v, "max-age=31536000") {
		t.Fatalf("HSTS missing in remote mode: %q", v)
	}
}

func TestProblemCarriesRequestID(t *testing.T) {
	w := do(newHandler(false, nil), "GET", "/api/v1/system/info", nil)
	var p map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p["requestId"] == "" || p["requestId"] != w.Header().Get("X-Request-Id") {
		t.Fatalf("requestId %v does not match header %q", p["requestId"], w.Header().Get("X-Request-Id"))
	}
}

func TestBodyLimits(t *testing.T) {
	h := newHandler(true, nil)
	big := strings.NewReader(strings.Repeat("x", 5000))
	r := httptest.NewRequest("POST", "/api/v1/auth/login", big)
	r.ContentLength = 5000
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("auth body > 4 KiB must be refused, got %d", w.Code)
	}
}

func TestGzipCompressesLargeJSONOnly(t *testing.T) {
	big := strings.Repeat("a", 4096)
	h := gzipMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("small") != "" {
			w.Header().Set("Content-Length", "2")
			_, _ = w.Write([]byte("{}"))
			return
		}
		_, _ = w.Write([]byte(`{"v":"` + big + `"}`))
	}))
	w := do(h, "GET", "/x", map[string]string{"Accept-Encoding": "gzip"})
	if w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Vary") == "" {
		t.Fatalf("large JSON should be gzipped: %v", w.Header())
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(zr)
	if !strings.Contains(string(out), big) {
		t.Fatal("round trip mismatch")
	}
	if w = do(h, "GET", "/x?small=1", map[string]string{"Accept-Encoding": "gzip"}); w.Header().Get("Content-Encoding") != "" {
		t.Fatal("tiny responses must not be compressed")
	}
	if w = do(h, "GET", "/x", nil); w.Header().Get("Content-Encoding") != "" {
		t.Fatal("no Accept-Encoding ⇒ no compression")
	}
	if w = do(h, "GET", "/x", map[string]string{"Accept-Encoding": "gzip", "Range": "bytes=0-9"}); w.Header().Get("Content-Encoding") != "" {
		t.Fatal("Range requests must not be compressed")
	}
}

func TestStaticETagConditionalRequest(t *testing.T) {
	h := newHandler(false, nil)
	w := do(h, "GET", "/", nil)
	et := w.Header().Get("ETag")
	if et == "" {
		t.Fatal("missing ETag on app shell")
	}
	if w2 := do(h, "GET", "/", map[string]string{"If-None-Match": et}); w2.Code != http.StatusNotModified {
		t.Fatalf("conditional request should be 304, got %d", w2.Code)
	}
}

func TestHostFactsAreCached(t *testing.T) {
	calls := 0
	clk := clock.NewFake(time.Now())
	cfg := config.Default()
	h := New(Options{Config: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Clock: clk, Demo: true, UI: testUI(),
		HostFacts: func() hostFacts { calls++; return hostFacts{Hostname: "h", Capabilities: map[string]bool{}} }})
	for i := 0; i < 5; i++ {
		do(h, "GET", "/api/v1/system/info", nil)
	}
	if calls != 1 {
		t.Fatalf("host probed %d times within TTL, want 1", calls)
	}
	clk.Advance(factsTTL + time.Second)
	do(h, "GET", "/api/v1/system/info", nil)
	if calls != 2 {
		t.Fatalf("expected re-probe after TTL, calls=%d", calls)
	}
}

func TestPanicAfterPartialWriteDoesNotDoubleWriteHeader(t *testing.T) {
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, nil))
	h := chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("partial"))
		panic("late")
	}), requestID, recoverer(log))
	w := do(h, "GET", "/x", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "internal") {
		t.Fatalf("response already started must not be rewritten: %d %q", w.Code, w.Body)
	}
	if !strings.Contains(buf.String(), "late") {
		t.Fatal("panic must still be logged")
	}
}

func TestHostGuardIPv6(t *testing.T) {
	h := newHandler(false, func(c *config.Config) { c.Server.AllowedHosts = []string{"::1"} })
	r := httptest.NewRequest("GET", "/healthz", nil)
	r.Host = "[::1]:8443"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("bracketed IPv6 host with port should match, got %d", w.Code)
	}
}

func TestHeadOnSPA(t *testing.T) {
	if w := do(newHandler(false, nil), "HEAD", "/", nil); w.Code != 200 {
		t.Fatalf("HEAD / = %d", w.Code)
	}
}
