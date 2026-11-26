package api

import (
	"net/http"
	"strings"

	"github.com/serpis172/Yubeos/internal/auth"
	"github.com/serpis172/Yubeos/internal/problem"
)

// authed rebuilds the Authenticated value the auth service expects. ok is false in demo mode (no session).
func authed(r *http.Request) (a *auth.Authenticated, ok bool) {
	p, _ := PrincipalFrom(r.Context())
	if p.Session == nil {
		return nil, false
	}
	return &auth.Authenticated{Session: *p.Session, Role: roleToIdentity(p.Role)}, true
}

var demoCodes = []string{"ABCD-EFGH-IJKL-MNOP", "QRST-UVWX-YZ23-4567", "AAAA-BBBB-CCCC-DDDD", "EEEE-FFFF-GGGG-HHHH", "IIII-JJJJ-KKKK-LLLL",
	"MMMM-NNNN-OOOO-PPPP", "QQQQ-RRRR-SSSS-TTTT", "UUUU-VVVV-WWWW-XXXX", "YYYY-ZZZZ-2222-3333", "4444-5555-6666-7777"}

func (s *Server) verify2FA(w http.ResponseWriter, r *http.Request) {
	if s.opts.Auth == nil {
		problem.Unauthenticated(w, r, "authentication is not configured")
		return
	}
	if !s.preAuthGuard(w, r) {
		return
	}
	var req struct {
		ChallengeID string `json:"challengeId"`
		Method      string `json:"method"`
		Code        string `json:"code"`
		TrustDevice bool   `json:"trustDevice"` // accepted; trusted devices are not implemented yet
		DeviceLabel string `json:"deviceLabel"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ChallengeID == "" || len(req.Code) > 64 {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation", "Invalid input", "challengeId and code are required")
		return
	}
	res, err := s.opts.Auth.VerifySecondFactor(r.Context(), req.ChallengeID, strings.ToLower(req.Method), req.Code, s.client(r))
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	setSessionCookie(w, res.Token, res.Session.ExpiresAt)
	writeJSON(w, http.StatusOK, meOf(res.Session, string(res.Role)))
}

func (s *Server) factors(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	required := false
	switch s.opts.Config.Auth.Require2FA {
	case "all":
		required = true
	case "admins":
		required = p.Role == RoleAdmin
	}
	out := map[string]any{"totp": false, "passkeys": []any{}, "recoveryCodesLeft": 0, "required": required}
	if a, ok := authed(r); ok {
		totp, left, err := s.opts.Auth.Factors(r.Context(), a.Session.Username)
		if err != nil {
			s.writeAuthError(w, r, err)
			return
		}
		out["totp"], out["recoveryCodesLeft"] = totp, left
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) totpEnroll(w http.ResponseWriter, r *http.Request) {
	a, ok := authed(r)
	if !ok { // demo: a fixed example secret so the wizard can be built
		writeJSON(w, http.StatusOK, map[string]string{"secret": "JBSWY3DPEHPK3PXP", "otpauthUri": "otpauth://totp/Yubeos:demo?secret=JBSWY3DPEHPK3PXP&issuer=Yubeos"})
		return
	}
	secret, uri, err := s.opts.Auth.TOTPBegin(r.Context(), a, s.client(r))
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "otpauthUri": uri})
}

func (s *Server) totpConfirm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Code) < 6 || len(req.Code) > 16 {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation", "Invalid input", "enter the 6-digit code",
			problem.FieldError{Pointer: "/code", Message: "6 digits"})
		return
	}
	a, ok := authed(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"codes": demoCodes})
		return
	}
	codes, err := s.opts.Auth.TOTPConfirm(r.Context(), a, req.Code, s.client(r))
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"codes": codes})
}

func (s *Server) totpRemove(w http.ResponseWriter, r *http.Request) {
	a, ok := authed(r)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.opts.Auth.TOTPRemove(r.Context(), a, s.client(r)); err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) recoveryRegenerate(w http.ResponseWriter, r *http.Request) {
	a, ok := authed(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"codes": demoCodes})
		return
	}
	codes, err := s.opts.Auth.RegenerateRecovery(r.Context(), a, s.client(r))
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"codes": codes})
}

func (s *Server) stepUpBegin(w http.ResponseWriter, r *http.Request) {
	a, ok := authed(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"challengeId": "demo", "methods": []string{"totp", "recovery"}})
		return
	}
	ch, err := s.opts.Auth.StepUpBegin(r.Context(), a, s.client(r))
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"challengeId": ch.ID, "methods": ch.Methods, "expiresAt": ch.Expires})
}

func (s *Server) stepUp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChallengeID string         `json:"challengeId"`
		Method      string         `json:"method"`
		Code        string         `json:"code"`
		Password    string         `json:"password"`
		Credential  map[string]any `json:"credential"` // passkeys: not implemented, the method is refused below
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ChallengeID == "" || len(req.Code) > 64 || len(req.Password) > 4096 {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation", "Invalid input", "challengeId and a code or password are required")
		return
	}
	a, ok := authed(r)
	if !ok { // demo
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.opts.Auth.StepUp(r.Context(), a, req.ChallengeID, strings.ToLower(req.Method), req.Code, req.Password, s.client(r)); err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
