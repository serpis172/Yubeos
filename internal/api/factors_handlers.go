package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/serpis172/Yubeos/internal/auth"
	"github.com/serpis172/Yubeos/internal/problem"
	"github.com/serpis172/Yubeos/internal/webauthn"
)

const deviceCookieName = auth.DeviceCookie

func (s *Server) registerFactorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/2fa/verify", s.loginSecondFactor)
	mux.HandleFunc("POST /api/v1/auth/webauthn/login/begin", s.webauthnLoginBegin)
	mux.HandleFunc("POST /api/v1/auth/webauthn/login/finish", s.webauthnLoginFinish)

	mux.Handle("GET /api/v1/auth/factors", s.require(RoleUser, s.factors, allowEnrollment()))
	mux.Handle("POST /api/v1/auth/totp/enroll", s.require(RoleUser, s.totpBegin, allowEnrollment()))
	mux.Handle("POST /api/v1/auth/totp/confirm", s.require(RoleUser, s.totpConfirm, allowEnrollment()))
	mux.Handle("DELETE /api/v1/auth/totp", s.require(RoleUser, s.totpRemove, stepUp()))
	mux.Handle("POST /api/v1/auth/webauthn/register/begin", s.require(RoleUser, s.passkeyRegisterBegin, allowEnrollment()))
	mux.Handle("POST /api/v1/auth/webauthn/register/finish", s.require(RoleUser, s.passkeyRegisterFinish, allowEnrollment()))
	mux.Handle("DELETE /api/v1/auth/webauthn/{id}", s.require(RoleUser, s.passkeyRemove, stepUp()))
	mux.Handle("POST /api/v1/auth/recovery-codes:regenerate", s.require(RoleUser, s.recoveryRegenerate, stepUp()))
	mux.Handle("POST /api/v1/auth/stepup/begin", s.require(RoleUser, s.stepUpBegin, allowEnrollment()))
	mux.Handle("POST /api/v1/auth/stepup", s.require(RoleUser, s.stepUp, allowEnrollment()))
	mux.Handle("GET /api/v1/security/remote-status", s.require(RoleAdmin, s.remoteStatus))
	mux.Handle("POST /api/v1/security/lockdown", s.require(RoleAdmin, s.setLockdown, stepUp()))
}

// ---- wire formats ------------------------------------------------------------------------------------------------

type challengeDTO struct {
	ChallengeID string         `json:"challengeId"`
	Methods     []string       `json:"methods"`
	WebAuthn    map[string]any `json:"webauthn,omitempty"`
	ExpiresAt   time.Time      `json:"expiresAt"`
}

type needs2faDTO struct {
	Needs2FA    bool     `json:"needs2fa"`
	Methods     []string `json:"methods"`
	ChallengeID string   `json:"challengeId"`
}

func toNeeds2FA(c *auth.Challenge) needs2faDTO {
	m := c.Methods
	if m == nil {
		m = []string{}
	}
	return needs2faDTO{Needs2FA: true, Methods: m, ChallengeID: c.ID}
}

func toChallengeDTO(c *auth.Challenge) challengeDTO {
	m := c.Methods
	if m == nil {
		m = []string{}
	}
	return challengeDTO{ChallengeID: c.ID, Methods: m, WebAuthn: c.WebAuthn, ExpiresAt: c.ExpiresAt}
}

type assertionJSON struct {
	ID       string `json:"id"`
	RawID    string `json:"rawId"`
	Response struct {
		AuthenticatorData string `json:"authenticatorData"`
		ClientDataJSON    string `json:"clientDataJSON"`
		Signature         string `json:"signature"`
	} `json:"response"`
}

func (a *assertionJSON) decode() (*auth.Assertion, bool) {
	if a == nil {
		return nil, false
	}
	var out auth.Assertion
	var e1, e2, e3, e4 error
	out.RawID, e1 = webauthn.FromB64(a.RawID)
	out.AuthenticatorData, e2 = webauthn.FromB64(a.Response.AuthenticatorData)
	out.ClientDataJSON, e3 = webauthn.FromB64(a.Response.ClientDataJSON)
	out.Signature, e4 = webauthn.FromB64(a.Response.Signature)
	return &out, e1 == nil && e2 == nil && e3 == nil && e4 == nil
}

type attestationJSON struct {
	ID       string `json:"id"`
	RawID    string `json:"rawId"`
	Response struct {
		AttestationObject string `json:"attestationObject"`
		ClientDataJSON    string `json:"clientDataJSON"`
	} `json:"response"`
}

func (a *attestationJSON) decode() (*auth.Attestation, bool) {
	if a == nil {
		return nil, false
	}
	var out auth.Attestation
	var e1, e2 error
	out.AttestationObject, e1 = webauthn.FromB64(a.Response.AttestationObject)
	out.ClientDataJSON, e2 = webauthn.FromB64(a.Response.ClientDataJSON)
	return &out, e1 == nil && e2 == nil
}

func setDeviceCookie(w http.ResponseWriter, token string, days int) {
	if days <= 0 {
		days = 1
	}
	http.SetCookie(w, &http.Cookie{Name: deviceCookieName, Value: token, Path: "/", MaxAge: days * 86400, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

// writeLoginResult answers a completed login: session cookie, optional trusted-device cookie, and the me payload.
func (s *Server) writeLoginResult(w http.ResponseWriter, res *auth.LoginResult) {
	setSessionCookie(w, res.Token, res.Session.ExpiresAt)
	if res.DeviceToken != "" {
		setDeviceCookie(w, res.DeviceToken, s.opts.Config.Auth.TrustedDeviceDays)
	}
	writeJSON(w, http.StatusOK, meOf(res.Session, string(res.Role)))
}

func (s *Server) writeFactorError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidChallenge):
		problem.Write(w, r, http.StatusBadRequest, "invalid-challenge", "Challenge expired or invalid", "start the sign-in again")
	case errors.Is(err, auth.ErrInvalidSecondFactor):
		problem.Write(w, r, http.StatusUnauthorized, "invalid-second-factor", "Code or passkey rejected", "try again")
	case errors.Is(err, auth.ErrStepUpRequired):
		problem.Write(w, r, http.StatusForbidden, "step-up-required", "Confirm it is you", "re-authenticate with your second factor")
	case errors.Is(err, auth.ErrLastFactor):
		problem.Write(w, r, http.StatusConflict, "last-factor", "Keep at least one second factor", "policy requires a second factor for your account")
	case errors.Is(err, auth.ErrPasskeyNotSole):
		problem.Write(w, r, http.StatusForbidden, "passkey-not-sole", "Passkeys alone are disabled", "sign in with your password and a passkey as second factor")
	case errors.Is(err, auth.ErrFlagged):
		problem.Write(w, r, http.StatusForbidden, "passkey-flagged", "Passkey blocked", "this passkey showed signs of cloning; remove it and enrol a new one")
	case errors.Is(err, auth.ErrAlreadyEnrolled):
		problem.Write(w, r, http.StatusConflict, "already-enrolled", "Already enrolled", "remove the existing factor first")
	case isWebAuthnErr(err):
		problem.Write(w, r, http.StatusBadRequest, "webauthn-invalid", "Passkey response rejected", err.Error())
	default:
		s.writeAuthError(w, r, err)
	}
}

func isWebAuthnErr(err error) bool {
	for _, e := range []error{webauthn.ErrChallenge, webauthn.ErrOrigin, webauthn.ErrFlags, webauthn.ErrSignature, webauthn.ErrUnsupported, webauthn.ErrMalformed, webauthn.ErrCounter} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// ---- login -------------------------------------------------------------------------------------------------------

func (s *Server) loginSecondFactor(w http.ResponseWriter, r *http.Request) {
	if s.opts.Auth == nil {
		problem.Unauthenticated(w, r, "authentication is not configured")
		return
	}
	if !s.preAuthGuard(w, r) {
		return
	}
	var req secondFactorReq
	if !decodeJSON(w, r, &req) {
		return
	}
	sf, ok := req.factor()
	if !ok {
		problem.Write(w, r, http.StatusBadRequest, "invalid-json", "Invalid passkey response", "")
		return
	}
	res, err := s.opts.Auth.CompleteLogin(r.Context(), req.ChallengeID, sf, req.TrustDevice, s.client(r))
	if err != nil {
		s.writeFactorError(w, r, err)
		return
	}
	s.writeLoginResult(w, res)
}

func (s *Server) webauthnLoginBegin(w http.ResponseWriter, r *http.Request) {
	if s.opts.Auth == nil {
		problem.Unauthenticated(w, r, "authentication is not configured")
		return
	}
	if !s.preAuthGuard(w, r) {
		return
	}
	var req struct {
		ChallengeID string `json:"challengeId"`
	}
	if r.ContentLength != 0 && !decodeJSON(w, r, &req) {
		return
	}
	ch, err := s.opts.Auth.WebAuthnLoginBegin(r.Context(), req.ChallengeID, s.client(r))
	if err != nil {
		s.writeFactorError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toChallengeDTO(ch))
}

func (s *Server) webauthnLoginFinish(w http.ResponseWriter, r *http.Request) {
	if s.opts.Auth == nil {
		problem.Unauthenticated(w, r, "authentication is not configured")
		return
	}
	if !s.preAuthGuard(w, r) {
		return
	}
	var req struct {
		ChallengeID string         `json:"challengeId"`
		TrustDevice bool           `json:"trustDevice"`
		Credential  *assertionJSON `json:"credential"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	a, ok := req.Credential.decode()
	if !ok {
		problem.Write(w, r, http.StatusBadRequest, "invalid-json", "Invalid passkey response", "")
		return
	}
	res, err := s.opts.Auth.WebAuthnLoginFinish(r.Context(), req.ChallengeID, a, req.TrustDevice, s.client(r))
	if err != nil {
		s.writeFactorError(w, r, err)
		return
	}
	s.writeLoginResult(w, res)
}

type secondFactorReq struct {
	ChallengeID string         `json:"challengeId"`
	Method      string         `json:"method"`
	Code        string         `json:"code"`
	Password    string         `json:"password"`
	TrustDevice bool           `json:"trustDevice"`
	DeviceLabel string         `json:"deviceLabel"`
	Credential  *assertionJSON `json:"credential"`
}

func (q *secondFactorReq) factor() (auth.SecondFactor, bool) {
	sf := auth.SecondFactor{Method: q.Method, Code: q.Code}
	if q.Method == "webauthn" {
		a, ok := q.Credential.decode()
		if !ok {
			return sf, false
		}
		sf.Assertion = a
	}
	return sf, true
}

// ---- enrolment and management ------------------------------------------------------------------------------------

func (s *Server) authed(r *http.Request) (*auth.Authenticated, Principal) {
	p, _ := PrincipalFrom(r.Context())
	if p.Session == nil {
		return nil, p
	}
	return &auth.Authenticated{Session: *p.Session, Role: roleToIdentity(p.Role)}, p
}

type passkeyDTO struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	Flagged    bool       `json:"flagged"`
}

func toPasskeyDTO(p auth.Passkey) passkeyDTO {
	d := passkeyDTO{ID: p.ID, Label: p.Label, CreatedAt: p.CreatedAt, Flagged: p.Flagged}
	if !p.LastUsedAt.IsZero() {
		t := p.LastUsedAt
		d.LastUsedAt = &t
	}
	return d
}

func (s *Server) factors(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authed(r)
	if a == nil { // demo
		writeJSON(w, http.StatusOK, map[string]any{"totp": false, "passkeys": []passkeyDTO{}, "recoveryCodesLeft": 0, "required": false})
		return
	}
	f, err := s.opts.Auth.Factors(r.Context(), a)
	if err != nil {
		s.writeFactorError(w, r, err)
		return
	}
	pk := make([]passkeyDTO, 0, len(f.Passkeys))
	for _, p := range f.Passkeys {
		pk = append(pk, toPasskeyDTO(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"totp": f.TOTP, "passkeys": pk, "recoveryCodesLeft": f.RecoveryLeft, "required": f.Required})
}

func (s *Server) totpBegin(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authed(r)
	if a == nil {
		problem.NotFound(w, r)
		return
	}
	secret, uri, err := s.opts.Auth.TOTPBegin(r.Context(), a, s.client(r))
	if err != nil {
		s.writeFactorError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "otpauthUri": uri})
}

func (s *Server) totpConfirm(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authed(r)
	if a == nil {
		problem.NotFound(w, r)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	codes, err := s.opts.Auth.TOTPConfirm(r.Context(), a, req.Code, s.client(r))
	if err != nil {
		s.writeFactorError(w, r, err)
		return
	}
	if codes == nil {
		codes = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"codes": codes})
}

func (s *Server) totpRemove(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authed(r)
	if a == nil {
		problem.NotFound(w, r)
		return
	}
	if err := s.opts.Auth.TOTPRemove(r.Context(), a, s.client(r)); err != nil {
		s.writeFactorError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) recoveryRegenerate(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authed(r)
	if a == nil {
		problem.NotFound(w, r)
		return
	}
	codes, err := s.opts.Auth.RecoveryRegenerate(r.Context(), a, s.client(r))
	if err != nil {
		s.writeFactorError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"codes": codes})
}

func (s *Server) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authed(r)
	if a == nil {
		problem.NotFound(w, r)
		return
	}
	ch, err := s.opts.Auth.PasskeyRegisterBegin(r.Context(), a, s.client(r))
	if err != nil {
		s.writeFactorError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toChallengeDTO(ch))
}

func (s *Server) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authed(r)
	if a == nil {
		problem.NotFound(w, r)
		return
	}
	var req struct {
		ChallengeID string           `json:"challengeId"`
		Label       string           `json:"label"`
		Credential  *attestationJSON `json:"credential"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	att, ok := req.Credential.decode()
	if !ok {
		problem.Write(w, r, http.StatusBadRequest, "invalid-json", "Invalid passkey response", "")
		return
	}
	pk, err := s.opts.Auth.PasskeyRegisterFinish(r.Context(), a, req.ChallengeID, req.Label, att, s.client(r))
	if err != nil {
		s.writeFactorError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toPasskeyDTO(*pk))
}

func (s *Server) passkeyRemove(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authed(r)
	if a == nil {
		problem.NotFound(w, r)
		return
	}
	ok, err := s.opts.Auth.RemovePasskey(r.Context(), a, r.PathValue("id"), s.client(r))
	if err != nil {
		s.writeFactorError(w, r, err)
		return
	}
	if !ok {
		problem.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) stepUpBegin(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authed(r)
	if a == nil {
		writeJSON(w, http.StatusOK, challengeDTO{ChallengeID: "demo", Methods: []string{"password"}})
		return
	}
	ch, err := s.opts.Auth.StepUpBegin(r.Context(), a, s.client(r))
	if err != nil {
		s.writeFactorError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toChallengeDTO(ch))
}

func (s *Server) stepUp(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authed(r)
	if a == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var req secondFactorReq
	if !decodeJSON(w, r, &req) {
		return
	}
	sf, ok := req.factor()
	if !ok {
		problem.Write(w, r, http.StatusBadRequest, "invalid-json", "Invalid passkey response", "")
		return
	}
	if err := s.opts.Auth.StepUp(r.Context(), a, req.ChallengeID, req.Password, sf, s.client(r)); err != nil {
		s.writeFactorError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
