package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/serpis172/Yubeos/internal/auth"
	"github.com/serpis172/Yubeos/internal/identity"
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
	Username       string
	Role           Role
	Session        *auth.Session // nil in demo mode
	EnrollmentOnly bool          // session may only complete second-factor enrollment (docs/AUTHENTICATION.md §4)
}

// PrincipalFrom returns the authenticated principal, if any.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(keyPrincipal).(Principal)
	return p, ok
}

const sessionCookie = "__Host-yubeos_session"

type routeOpt func(*routeOpts)

type routeOpts struct{ allowEnrollment, stepUp bool }

// stepUp requires a recent second-factor re-authentication (POST /auth/stepup) on the session.
func stepUp() routeOpt { return func(o *routeOpts) { o.stepUp = true } }

// allowEnrollment lets an enrollment-only session reach the route (me, logout, factor enrolment, password).
func allowEnrollment() routeOpt { return func(o *routeOpts) { o.allowEnrollment = true } }

// require is the ONLY way to register an authenticated route: routes are default-deny.
//
// With a session cookie it validates the session, applies CSRF + Origin checks on state-changing methods,
// blocks enrollment-only sessions from everything but allowEnrollment routes, and enforces the minimum role.
// Demo mode yields a fake admin; without an auth service every route answers 401.
func (s *Server) require(min Role, h http.HandlerFunc, opts ...routeOpt) http.Handler {
	var ro routeOpts
	for _, o := range opts {
		o(&ro)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.opts.Demo {
			h(w, r.WithContext(context.WithValue(r.Context(), keyPrincipal, Principal{Username: "demo", Role: RoleAdmin})))
			return
		}
		if s.opts.Auth == nil {
			problem.Unauthenticated(w, r, "authentication is not configured")
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil || c.Value == "" {
			problem.Unauthenticated(w, r, "sign in required")
			return
		}
		a, err := s.opts.Auth.Authenticate(r.Context(), c.Value)
		if err != nil {
			clearSessionCookie(w)
			problem.Unauthenticated(w, r, "session expired or revoked")
			return
		}
		if a.NewToken != "" {
			setSessionCookie(w, a.NewToken, a.Session.ExpiresAt)
		}
		if isUnsafe(r.Method) {
			if !s.sameOrigin(r) {
				problem.Write(w, r, http.StatusForbidden, "csrf", "Cross-site request refused", "Origin check failed")
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Yubeos-CSRF")), []byte(a.Session.CSRF)) != 1 {
				problem.Write(w, r, http.StatusForbidden, "csrf", "CSRF token missing or invalid", "send the token from /auth/me in X-Yubeos-CSRF")
				return
			}
		}
		p := Principal{Username: a.Session.Username, Role: Role(a.Role), Session: &a.Session, EnrollmentOnly: a.Session.EnrollmentRequired}
		if p.EnrollmentOnly && !ro.allowEnrollment {
			problem.Write(w, r, http.StatusForbidden, "enrollment-required", "Second-factor enrollment required", "enrol a second factor before using the rest of Yubeos")
			return
		}
		if ro.stepUp && !s.opts.Auth.HasStepUp(&a.Session) {
			problem.Write(w, r, http.StatusForbidden, "step-up-required", "Confirm it is you", "re-authenticate with your second factor, then retry")
			return
		}
		if min == RoleAdmin && p.Role != RoleAdmin {
			problem.Forbidden(w, r, "administrator role required")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), keyPrincipal, p)))
	})
}

func isUnsafe(m string) bool {
	return m != http.MethodGet && m != http.MethodHead && m != http.MethodOptions
}

// sameOrigin is the Origin / Fetch-Metadata half of CSRF protection (docs/SECURITY.md §3).
func (s *Server) sameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || u.Host == "" {
			return false // includes "null"
		}
		if strings.EqualFold(u.Host, r.Host) {
			return true
		}
		for _, h := range s.opts.Config.Server.AllowedHosts {
			if strings.EqualFold(u.Hostname(), strings.Trim(h, "[]")) {
				return true
			}
		}
		return false
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return true
	}
	return false
}

// secure reports whether the request arrived over TLS (directly, or via a trusted TLS-terminating proxy).
func (s *Server) secure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return s.opts.Config.Server.BehindProxy && s.cls.PeerTrusted(r) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	ttl := int(time.Until(expires).Seconds())
	if ttl < 1 {
		ttl = 1
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", MaxAge: ttl, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

func roleOf(r identity.Role) string { return string(r) }

func roleToIdentity(r Role) identity.Role { return identity.Role(r) }
