package api

import (
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/serpis172/Yubeos/internal/netx"
	"github.com/serpis172/Yubeos/internal/problem"
)

// lockdown is the emergency switch of docs/REMOTE-ACCESS.md: while the flag file exists (created by
// `yubeosctl lockdown on`, as root), every request from a remote origin is refused. The file is polled at most
// once per second.
type lockdown struct {
	path string
	mu   sync.Mutex
	at   time.Time
	on   bool
}

func (l *lockdown) Active() bool {
	if l == nil || l.path == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Since(l.at) > time.Second {
		_, err := os.Stat(l.path)
		l.on, l.at = err == nil, time.Now()
	}
	return l.on
}

func lockdownGuard(l *lockdown, c *netx.Classifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if l == nil || l.path == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if l.Active() {
				if _, origin := c.Resolve(r); origin == netx.Remote {
					w.Header().Set("Retry-After", "600")
					problem.Write(w, r, http.StatusServiceUnavailable, "lockdown", "Remote access is disabled", "an administrator enabled lockdown mode")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
