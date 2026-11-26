package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/serpis172/Yubeos/internal/auth"
	"github.com/serpis172/Yubeos/internal/netx"
	"github.com/serpis172/Yubeos/internal/problem"
)

type meResponse struct {
	Username           string   `json:"username"`
	Role               string   `json:"role"`
	CSRF               string   `json:"csrf"`
	Factors            []string `json:"factors"`
	TrustedDevice      bool     `json:"trustedDevice"`
	RemoteSession      bool     `json:"remoteSession"`
	EnrollmentRequired bool     `json:"enrollmentRequired"`
}

func (s *Server) registerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/auth/setup", s.setup)
	mux.Handle("POST /api/v1/auth/logout", s.require(RoleUser, s.logout, allowEnrollment()))
	mux.Handle("GET /api/v1/auth/me", s.require(RoleUser, s.me, allowEnrollment()))
	mux.Handle("PUT /api/v1/auth/password", s.require(RoleUser, s.changePassword, allowEnrollment()))
	mux.Handle("GET /api/v1/auth/devices", s.require(RoleUser, s.listDevices, allowEnrollment()))
	mux.Handle("DELETE /api/v1/auth/devices/{id}", s.require(RoleUser, s.revokeDevice, allowEnrollment()))
	mux.Handle("POST /api/v1/auth/devices:revokeAll", s.require(RoleUser, s.revokeAll, allowEnrollment()))
}

func (s *Server) client(r *http.Request) auth.Client {
	ip, origin := s.cls.Resolve(r)
	ips := ""
	if ip != nil {
		ips = ip.String()
	}
	return auth.Client{IP: ips, Origin: origin, UserAgent: r.UserAgent(), ReqID: RequestID(r.Context())}
}

// decodeJSON strictly decodes a small JSON body into v; it writes the problem response and returns false on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			problem.Write(w, r, http.StatusRequestEntityTooLarge, "payload-too-large", "Request body too large", "")
		} else {
			problem.Write(w, r, http.StatusBadRequest, "invalid-json", "Invalid request body", "expected a JSON object with the documented fields")
		}
		return false
	}
	return true
}

// preAuthGuard applies the checks shared by endpoints that run before a session exists (login, setup):
// TLS only, and an Origin / Fetch-Metadata check (login CSRF).
func (s *Server) preAuthGuard(w http.ResponseWriter, r *http.Request) bool {
	if !s.secure(r) {
		problem.Write(w, r, http.StatusBadRequest, "tls-required", "HTTPS required", "sign in over HTTPS")
		return false
	}
	if !s.sameOrigin(r) {
		problem.Write(w, r, http.StatusForbidden, "csrf", "Cross-site request refused", "Origin check failed")
		return false
	}
	return true
}

// retryAfter sets Retry-After relative to the server clock (which is injectable in tests).
func (s *Server) retryAfter(w http.ResponseWriter, until time.Time) {
	if secs := int(until.Sub(s.opts.Clock.Now()).Seconds()) + 1; secs > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(secs))
	}
}

// writeAuthError maps service errors to problem responses. Credential failures are deliberately uniform.
func (s *Server) writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	var (
		le *auth.LockedError
		be *auth.BannedError
		we *auth.WeakPasswordError
	)
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		problem.Write(w, r, http.StatusUnauthorized, "invalid-credentials", "Invalid credentials", "check the username and password")
	case errors.As(err, &le):
		s.retryAfter(w, le.Until)
		problem.Write(w, r, http.StatusTooManyRequests, "locked-out", "Too many failed attempts", "try again later")
	case errors.As(err, &be):
		s.retryAfter(w, be.Until)
		problem.Write(w, r, http.StatusForbidden, "ip-banned", "Address temporarily blocked", "too many failed attempts from this address")
	case errors.Is(err, auth.ErrEnrollmentRequiredLocal):
		problem.Write(w, r, http.StatusForbidden, "enrollment-required-local", "Second factor required", "enrol a second factor from a local network before signing in remotely")
	case errors.Is(err, auth.ErrInvalidCode):
		problem.Write(w, r, http.StatusUnauthorized, "invalid-code", "Invalid code", "check the code and try again")
	case errors.Is(err, auth.ErrInvalidChallenge):
		problem.Write(w, r, http.StatusUnauthorized, "challenge-invalid", "Sign-in step expired", "start the sign-in again")
	case errors.Is(err, auth.ErrMethodNotAllowed):
		problem.Write(w, r, http.StatusBadRequest, "method-not-allowed", "Method not available", "use one of the methods offered for this step")
	case errors.Is(err, auth.ErrStepUpRequired):
		problem.Write(w, r, http.StatusForbidden, "step-up-required", "Confirmation required", "confirm with a second factor, then retry")
	case errors.Is(err, auth.ErrAlreadyEnrolled):
		problem.Write(w, r, http.StatusConflict, "already-enrolled", "Already enabled", "remove the current authenticator first")
	case errors.Is(err, auth.ErrNotEnrolled):
		problem.Write(w, r, http.StatusConflict, "not-enrolled", "Not enabled", "enable an authenticator first")
	case errors.Is(err, auth.ErrSecondFactorUnavailable):
		problem.Write(w, r, http.StatusNotImplemented, "second-factor-unavailable", "Second factor not available in this build", "")
	case errors.Is(err, auth.ErrPrivUnavailable):
		problem.Write(w, r, http.StatusServiceUnavailable, "priv-unavailable", "Privileged helper unavailable", "the yubeos-priv service is not reachable")
	case errors.Is(err, auth.ErrNotLocal):
		problem.Write(w, r, http.StatusForbidden, "local-origin-required", "Local network required", "this action is only allowed from a local network")
	case errors.Is(err, auth.ErrInvalidSetupToken):
		problem.Write(w, r, http.StatusBadRequest, "invalid-setup-link", "Setup link invalid or expired", "ask an administrator for a new one: yubeosctl user add <user>")
	case errors.As(err, &we):
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation", "Password rejected", we.Msg, problem.FieldError{Pointer: "/newPassword", Message: we.Msg})
	default:
		s.opts.Log.Error("auth error", "reqId", RequestID(r.Context()), "err", err)
		problem.Internal(w, r)
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.opts.Auth == nil {
		problem.Unauthenticated(w, r, "authentication is not configured")
		return
	}
	if !s.preAuthGuard(w, r) {
		return
	}
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		DeviceHint string `json:"deviceHint"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Username == "" || len(req.Username) > 64 || req.Password == "" || len(req.Password) > 4096 {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation", "Invalid input", "username and password are required",
			problem.FieldError{Pointer: "/username", Message: "1..64 characters"}, problem.FieldError{Pointer: "/password", Message: "required"})
		return
	}
	res, err := s.opts.Auth.Login(r.Context(), req.Username, req.Password, s.client(r))
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	if res.Challenge != nil { // password accepted, second factor needed
		writeJSON(w, http.StatusAccepted, map[string]any{"needs2fa": true, "methods": res.Challenge.Methods, "challengeId": res.Challenge.ID})
		return
	}
	setSessionCookie(w, res.Token, res.Session.ExpiresAt)
	writeJSON(w, http.StatusOK, meOf(res.Session, string(res.Role)))
}

func meOf(sess auth.Session, role string) meResponse {
	f := sess.Factors
	if f == nil {
		f = []string{}
	}
	return meResponse{Username: sess.Username, Role: role, CSRF: sess.CSRF, Factors: f,
		RemoteSession: sess.Origin == netx.Remote, EnrollmentRequired: sess.EnrollmentRequired}
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	if p.Session == nil { // demo
		writeJSON(w, http.StatusOK, meResponse{Username: p.Username, Role: string(p.Role), Factors: []string{}})
		return
	}
	writeJSON(w, http.StatusOK, meOf(*p.Session, string(p.Role)))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	if p.Session != nil {
		if c, err := r.Cookie(sessionCookie); err == nil {
			a := &auth.Authenticated{Session: *p.Session, Role: roleToIdentity(p.Role)}
			s.opts.Auth.Logout(r.Context(), a, c.Value, s.client(r))
		}
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if s.opts.Auth == nil {
		problem.Unauthenticated(w, r, "authentication is not configured")
		return
	}
	if !s.preAuthGuard(w, r) {
		return
	}
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"newPassword"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Token == "" || req.NewPassword == "" {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation", "Invalid input", "token and newPassword are required")
		return
	}
	if err := s.opts.Auth.Setup(r.Context(), req.Token, req.NewPassword, s.client(r)); err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if p.Session == nil { // demo: no credentials exist, nothing to change
		w.WriteHeader(http.StatusNoContent)
		return
	}
	a := &auth.Authenticated{Session: *p.Session, Role: roleToIdentity(p.Role)}
	if err := s.opts.Auth.ChangePassword(r.Context(), a, req.CurrentPassword, req.NewPassword, s.client(r)); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			problem.Write(w, r, http.StatusForbidden, "wrong-password", "Current password is incorrect", "")
			return
		}
		s.writeAuthError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type deviceDTO struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Label      string    `json:"label"`
	Browser    string    `json:"browser,omitempty"`
	OS         string    `json:"os,omitempty"`
	LastIP     string    `json:"lastIp,omitempty"`
	Current    bool      `json:"current"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	if p.Session == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []deviceDTO{}})
		return
	}
	list, err := s.opts.Auth.Sessions.List(r.Context(), p.Username)
	if err != nil {
		problem.Internal(w, r)
		return
	}
	out := make([]deviceDTO, 0, len(list))
	for _, si := range list {
		b, o := uaSummary(si.UserAgent)
		out = append(out, deviceDTO{ID: si.SID, Kind: "session", Label: b + " on " + o, Browser: b, OS: o, LastIP: si.IP,
			Current: si.SID == p.Session.SID, CreatedAt: si.CreatedAt, LastUsedAt: si.LastSeenAt, ExpiresAt: si.ExpiresAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	if p.Session == nil { // demo: no sessions exist
		w.WriteHeader(http.StatusNoContent)
		return
	}
	id := r.PathValue("id")
	ok, err := s.opts.Auth.Sessions.RevokeSID(r.Context(), p.Username, id) // scoped to the caller's own sessions
	if err != nil {
		problem.Internal(w, r)
		return
	}
	if !ok {
		problem.NotFound(w, r)
		return
	}
	if p.Session != nil && id == p.Session.SID {
		clearSessionCookie(w)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) revokeAll(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	if p.Session == nil { // demo: no sessions exist
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if _, err := s.opts.Auth.Sessions.RevokeUser(r.Context(), p.Username, ""); err != nil {
		problem.Internal(w, r)
		return
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
