package api

import (
	"context"
	"net/http"

	"github.com/serpis172/Yubeos/internal/problem"
)

// Role is an authorisation level (see docs/SECURITY.md §5).
type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

// Principal is the authenticated caller.
type Principal struct {
	Username string
	Role     Role
}

// PrincipalFrom returns the authenticated principal, if any.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(keyPrincipal).(Principal)
	return p, ok
}

// require is the ONLY way to register an authenticated route: routes are default-deny.
//
// Phase 0 has no real authentication yet (arrives in Phase 1, docs/ROADMAP.md). Until then, only demo mode
// yields a (fake admin) principal; every other deployment gets 401 — the safe default.
func (s *Server) require(min Role, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.authenticate(r)
		if !ok {
			problem.Unauthenticated(w, r, "authentication is not available in this build yet (Phase 1)")
			return
		}
		if min == RoleAdmin && p.Role != RoleAdmin {
			problem.Forbidden(w, r, "administrator role required")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), keyPrincipal, p)))
	})
}

func (s *Server) authenticate(*http.Request) (Principal, bool) {
	if s.opts.Demo {
		return Principal{Username: "demo", Role: RoleAdmin}, true
	}
	return Principal{}, false
}
