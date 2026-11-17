// Package netx resolves the real client address and classifies it as local or remote
// (docs/REMOTE-ACCESS.md §4). Proxy headers are honoured ONLY from configured trusted proxies.
package netx

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Origin string

const (
	Local  Origin = "local"
	Remote Origin = "remote"
)

var cgnat = mustCIDR("100.64.0.0/10") // Tailscale overlay and ISP carrier-grade NAT

func mustCIDR(s string) *net.IPNet { _, n, _ := net.ParseCIDR(s); return n }

// ParseNets parses IPs and CIDRs ("10.0.0.5", "10.0.0.0/8", "::1") into networks.
func ParseNets(list []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, s := range list {
		if _, n, err := net.ParseCIDR(s); err == nil {
			out = append(out, n)
			continue
		}
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, &net.ParseError{Type: "IP or CIDR", Text: s}
		}
		bits := 128
		if ip4 := ip.To4(); ip4 != nil {
			ip, bits = ip4, 32
		}
		out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return out, nil
}

func contains(nets []*net.IPNet, ip net.IP) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Classifier resolves and classifies client addresses.
type Classifier struct {
	Trusted      []*net.IPNet // proxies allowed to set forwarding headers
	OverlayLocal bool         // treat 100.64.0.0/10 as local — only while a Tailscale interface exists
	Allow        []*net.IPNet // optional allow-list for REMOTE origins (empty = no restriction)

	// HasOverlay reports whether a Tailscale interface exists on this host (default: looks for "tailscale0", cached 30 s).
	HasOverlay func() bool

	mu     sync.Mutex
	cached bool
	at     time.Time
}

func (c *Classifier) overlay() bool {
	if c.HasOverlay != nil {
		return c.HasOverlay()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.at) > 30*time.Second {
		_, err := net.InterfaceByName("tailscale0")
		c.cached, c.at = err == nil, time.Now()
	}
	return c.cached
}

// ClientIP returns the client address of r. The TCP peer is used unless it is a trusted proxy; then the
// right-most address in X-Forwarded-For that is NOT itself a trusted proxy is used (spoofed left entries are ignored).
func (c *Classifier) ClientIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(strings.Trim(host, "[]"))
	if peer == nil {
		return nil
	}
	if !contains(c.Trusted, peer) {
		return peer
	}
	if v := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); v != "" {
		if ip := net.ParseIP(v); ip != nil {
			return ip
		}
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(hops[i]))
		if ip == nil {
			return peer // malformed chain: fall back to the proxy itself, never to attacker-supplied text
		}
		if !contains(c.Trusted, ip) {
			return ip
		}
	}
	return peer
}

// PeerTrusted reports whether the TCP peer of r is a configured trusted proxy.
func (c *Classifier) PeerTrusted(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && contains(c.Trusted, ip)
}

// Classify labels an address local or remote. An unparseable/nil address is Remote (fail closed).
func (c *Classifier) Classify(ip net.IP) Origin {
	if ip == nil {
		return Remote
	}
	switch {
	case ip.IsLoopback(), ip.IsPrivate(), ip.IsLinkLocalUnicast():
		return Local
	case c.OverlayLocal && cgnat.Contains(ip) && c.overlay():
		return Local
	}
	return Remote
}

// Permitted applies the optional allow-list: local origins always pass.
func (c *Classifier) Permitted(ip net.IP) bool {
	if len(c.Allow) == 0 || c.Classify(ip) == Local {
		return true
	}
	return contains(c.Allow, ip)
}

// Resolve returns the client IP and its origin for r.
func (c *Classifier) Resolve(r *http.Request) (net.IP, Origin) {
	ip := c.ClientIP(r)
	return ip, c.Classify(ip)
}
