package netx

import (
	"net"
	"net/http/httptest"
	"testing"
)

func cls(t *testing.T, trusted ...string) *Classifier {
	t.Helper()
	n, err := ParseNets(trusted)
	if err != nil {
		t.Fatal(err)
	}
	return &Classifier{Trusted: n, HasOverlay: func() bool { return false }}
}

func TestClientIPIgnoresForwardingHeadersFromUntrustedPeers(t *testing.T) {
	c := cls(t, "127.0.0.1")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.9:5555"
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	r.Header.Set("CF-Connecting-IP", "10.0.0.2")
	if ip := c.ClientIP(r); ip.String() != "203.0.113.9" {
		t.Fatalf("spoofed header from an untrusted peer was honoured: %v", ip)
	}
	if _, o := c.Resolve(r); o != Remote {
		t.Fatalf("origin must stay remote, got %s", o)
	}
}

func TestClientIPFromTrustedProxy(t *testing.T) {
	c := cls(t, "127.0.0.1", "10.0.0.0/24")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("X-Forwarded-For", "192.168.1.5, 203.0.113.7, 10.0.0.3") // attacker-prepended local-looking entry on the left
	if ip := c.ClientIP(r); ip.String() != "203.0.113.7" {
		t.Fatalf("must take the right-most non-proxy hop, got %v", ip)
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.7, garbage")
	if ip := c.ClientIP(r); ip.String() != "127.0.0.1" {
		t.Fatalf("a malformed chain must fall back to the proxy, got %v", ip)
	}
	r.Header.Del("X-Forwarded-For")
	r.Header.Set("CF-Connecting-IP", "198.51.100.4")
	if ip := c.ClientIP(r); ip.String() != "198.51.100.4" {
		t.Fatalf("CF-Connecting-IP from a trusted proxy: %v", ip)
	}
}

func TestClassify(t *testing.T) {
	c := cls(t)
	local := []string{"127.0.0.1", "::1", "10.1.2.3", "172.16.0.9", "192.168.0.7", "fd00::5", "169.254.1.1", "fe80::1"}
	remote := []string{"8.8.8.8", "203.0.113.1", "2001:db8::1", "100.64.0.5", "172.32.0.1"}
	for _, s := range local {
		if c.Classify(net.ParseIP(s)) != Local {
			t.Errorf("%s should be local", s)
		}
	}
	for _, s := range remote {
		if c.Classify(net.ParseIP(s)) != Remote {
			t.Errorf("%s should be remote (100.64/10 only counts with an overlay)", s)
		}
	}
	if c.Classify(nil) != Remote {
		t.Error("nil must fail closed")
	}
}

func TestOverlayRangeOnlyLocalWithTailscaleInterface(t *testing.T) {
	c := cls(t)
	c.OverlayLocal = true
	ip := net.ParseIP("100.101.102.103")
	if c.Classify(ip) != Remote {
		t.Fatal("without a tailscale interface the CGNAT range is untrusted")
	}
	c.HasOverlay = func() bool { return true }
	if c.Classify(ip) != Local {
		t.Fatal("with tailscale present the overlay range is local")
	}
	c.OverlayLocal = false
	if c.Classify(ip) != Remote {
		t.Fatal("treatOverlayAsLocal=false must win")
	}
}

func TestPermittedAllowList(t *testing.T) {
	c := cls(t)
	c.Allow, _ = ParseNets([]string{"203.0.113.0/24"})
	if !c.Permitted(net.ParseIP("203.0.113.7")) || c.Permitted(net.ParseIP("198.51.100.1")) {
		t.Fatal("allow-list must apply to remote addresses")
	}
	if !c.Permitted(net.ParseIP("192.168.1.2")) {
		t.Fatal("local origins always pass")
	}
}

func TestParseNets(t *testing.T) {
	n, err := ParseNets([]string{"10.0.0.5", "10.0.0.0/8", "::1", "2001:db8::/32"})
	if err != nil || len(n) != 4 || !n[0].Contains(net.ParseIP("10.0.0.5")) || n[0].Contains(net.ParseIP("10.0.0.6")) {
		t.Fatalf("%v %v", n, err)
	}
	if _, err := ParseNets([]string{"nope"}); err == nil {
		t.Fatal("invalid entry must error")
	}
}
