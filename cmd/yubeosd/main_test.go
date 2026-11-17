package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/serpis172/Yubeos/internal/config"
)

func TestRunVersion(t *testing.T) {
	if code := run([]string{"--version"}, nil); code != 0 {
		t.Fatalf("exit %d", code)
	}
}

func TestRunRejectsNoTLSOutsideDemo(t *testing.T) {
	if code := run([]string{"--no-tls"}, nil); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestRunRejectsBadConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(p, []byte(`{"server":{"nope":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--config", p}, nil); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if code := run([]string{"--config", "/does/not/exist.json"}, nil); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestRunRejectsUnknownFlag(t *testing.T) {
	if code := run([]string{"--bogus"}, nil); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestIsLoopbackListen(t *testing.T) {
	yes := []string{"127.0.0.1:8443", "localhost:8443", "[::1]:8443", "127.1.2.3:80"}
	no := []string{":8443", "0.0.0.0:8443", "[::]:8443", "192.168.1.10:443", "nas.lan:443", "garbage"}
	for _, a := range yes {
		if !isLoopbackListen(a) {
			t.Errorf("%q should be loopback", a)
		}
	}
	for _, a := range no {
		if isLoopbackListen(a) {
			t.Errorf("%q must NOT be treated as loopback", a)
		}
	}
}

func TestDemoRefusesPublicListenUnlessForced(t *testing.T) {
	if code := run([]string{"--demo", "--listen", ":0"}, nil); code != 2 {
		t.Fatalf("demo on all interfaces must be refused, exit=%d", code)
	}
}

func TestRedirectHandler(t *testing.T) {
	h := redirectHandler(":8443", []string{"nas.example.org"})
	r := httptest.NewRequest("GET", "/disks?x=1", nil)
	r.Host = "nas.example.org:80"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "https://nas.example.org:8443/disks?x=1" {
		t.Fatalf("redirect: %d %q", w.Code, w.Header().Get("Location"))
	}
	r = httptest.NewRequest("GET", "/", nil)
	r.Host = "evil.example"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("forged Host must not be redirected to, got %d %q", w.Code, w.Header().Get("Location"))
	}
	// no allow-list, standard port, IPv6
	h = redirectHandler(":443", nil)
	r = httptest.NewRequest("GET", "/", nil)
	r.Host = "[::1]:80"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Location") != "https://[::1]/" {
		t.Fatalf("IPv6 redirect: %q", w.Header().Get("Location"))
	}
}

func TestDisplayAddr(t *testing.T) {
	a, _ := net.ResolveTCPAddr("tcp", "0.0.0.0:8443")
	if got := displayAddr(a); got != "localhost:8443" {
		t.Fatalf("got %q", got)
	}
}

func TestNewCertManagerModes(t *testing.T) {
	dir := t.TempDir()
	if m, err := newCertManager(config.Server{TLS: config.TLS{Mode: "selfsigned"}}, dir); err != nil || m.Fingerprint() == "" {
		t.Fatalf("selfsigned: %v", err)
	}
	if _, err := newCertManager(config.Server{TLS: config.TLS{Mode: "files", CertFile: "/nope", KeyFile: "/nope"}}, dir); err == nil {
		t.Fatal("missing files must fail")
	}
	if _, err := newCertManager(config.Server{TLS: config.TLS{Mode: "acme"}}, dir); err == nil {
		t.Fatal("acme must fail until implemented")
	}
}
