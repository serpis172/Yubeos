// Package identity maps a Linux account to a Yubeos role (docs/AUTHENTICATION.md §1).
package identity

import (
	"os/user"
	"sync"
	"time"

	"github.com/serpis172/Yubeos/internal/clock"
)

type Role string

const (
	Admin Role = "admin"
	User  Role = "user"
)

// Info describes a resolved account. Role is empty when the account may not use Yubeos.
type Info struct {
	Username string
	UID      uint32
	Role     Role
}

// Resolver looks up Linux accounts.
type Resolver interface {
	Lookup(username string) (Info, bool)
}

// Linux resolves through os/user (reads /etc/passwd and /etc/group; no cgo, no NSS plugins).
type Linux struct {
	AdminGroup, UserGroup string
	AllowRoot             bool
}

func (l Linux) Lookup(username string) (Info, bool) {
	u, err := user.Lookup(username)
	if err != nil {
		return Info{}, false
	}
	var uid uint32
	for _, c := range u.Uid {
		if c < '0' || c > '9' {
			return Info{}, false
		}
		uid = uid*10 + uint32(c-'0')
	}
	if uid == 0 && !l.AllowRoot {
		return Info{Username: u.Username, UID: 0}, true // exists, but has no role
	}
	gids, err := u.GroupIds()
	if err != nil {
		return Info{Username: u.Username, UID: uid}, true
	}
	info := Info{Username: u.Username, UID: uid}
	for _, gid := range gids {
		g, err := user.LookupGroupId(gid)
		if err != nil {
			continue
		}
		switch g.Name {
		case l.AdminGroup:
			info.Role = Admin
		case l.UserGroup:
			if info.Role != Admin {
				info.Role = User
			}
		}
	}
	return info, true
}

// Cached wraps a Resolver with a short TTL so every request does not read /etc files (role changes still
// take effect within the TTL: docs/AUTHENTICATION.md §6 says 60 s).
type Cached struct {
	Inner Resolver
	TTL   time.Duration
	Clock clock.Clock

	mu sync.Mutex
	m  map[string]cacheEntry
}

type cacheEntry struct {
	info Info
	ok   bool
	at   time.Time
}

func (c *Cached) Lookup(username string) (Info, bool) {
	clk := c.Clock
	if clk == nil {
		clk = clock.Real{}
	}
	now := clk.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]cacheEntry{}
	}
	if e, hit := c.m[username]; hit && now.Sub(e.at) < c.TTL {
		return e.info, e.ok
	}
	info, ok := c.Inner.Lookup(username)
	c.m[username] = cacheEntry{info, ok, now}
	return info, ok
}

// Static is a fixed resolver for tests and demo mode.
type Static map[string]Info

func (s Static) Lookup(username string) (Info, bool) { i, ok := s[username]; return i, ok }
