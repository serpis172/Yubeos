package identity

import (
	"os/user"
	"testing"
	"time"

	"github.com/serpis172/Yubeos/internal/clock"
)

type counting struct {
	n    int
	info Info
}

func (c *counting) Lookup(string) (Info, bool) { c.n++; return c.info, true }

func TestCachedHonoursTTL(t *testing.T) {
	clk := clock.NewFake(time.Now())
	in := &counting{info: Info{Username: "a", Role: User}}
	c := &Cached{Inner: in, TTL: time.Minute, Clock: clk}
	c.Lookup("a")
	c.Lookup("a")
	if in.n != 1 {
		t.Fatalf("cache missed within TTL: %d lookups", in.n)
	}
	in.info.Role = Admin
	clk.Advance(61 * time.Second)
	if i, _ := c.Lookup("a"); i.Role != Admin || in.n != 2 {
		t.Fatalf("role change must be visible after the TTL: %+v n=%d", i, in.n)
	}
}

func TestLinuxLookupUnknownAndRootPolicy(t *testing.T) {
	l := Linux{AdminGroup: "yubeos-admin", UserGroup: "yubeos-user"}
	if _, ok := l.Lookup("definitely-not-a-user-xyz"); ok {
		t.Fatal("unknown user must not resolve")
	}
	if info, ok := l.Lookup("root"); !ok || info.Role != "" {
		t.Fatalf("root must exist but have no role unless allowRoot: %+v %v", info, ok)
	}
	cur, err := user.Current()
	if err == nil {
		if info, ok := l.Lookup(cur.Username); !ok || info.Username != cur.Username {
			t.Fatalf("current user lookup: %+v %v", info, ok)
		}
	}
}
