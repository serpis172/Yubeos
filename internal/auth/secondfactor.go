package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/serpis172/Yubeos/internal/audit"
	"github.com/serpis172/Yubeos/internal/identity"
	"github.com/serpis172/Yubeos/internal/priv"
)

// Second factors: TOTP and recovery codes (docs/AUTHENTICATION.md §4.1, §7). Secrets and verification live in the
// helper; this file only orchestrates challenges, sessions, throttling and audit. Passkeys and trusted devices are
// not implemented yet.

var (
	ErrInvalidChallenge = errors.New("auth: challenge invalid or expired")
	ErrInvalidCode      = errors.New("auth: invalid code")
	ErrStepUpRequired   = errors.New("auth: step-up required")
	ErrAlreadyEnrolled  = errors.New("auth: second factor already enabled")
	ErrNotEnrolled      = errors.New("auth: no second factor enabled")
	ErrMethodNotAllowed = errors.New("auth: method not available for this challenge")
)

const (
	challengeTTL      = 5 * time.Minute
	challengeAttempts = 5
	stepUpWindow      = 5 * time.Minute
	kindLogin         = "login"
	kindStepUp        = "stepup"
)

// Challenge is what the client gets back: an opaque single-use id and the methods it may answer with.
type Challenge struct {
	ID      string
	Methods []string
	Expires time.Time
}

type pending struct {
	kind    string
	user    string
	uid     uint32
	role    identity.Role
	epoch   int64
	sid     string // step-up: the session it belongs to
	c       Client
	methods []string
	exp     time.Time
	tries   int
}

type challenges struct {
	mu sync.Mutex
	m  map[string]*pending
}

// HelperFactors answers Has2FA from the helper store.
type HelperFactors struct{ Priv priv.Caller }

func (h HelperFactors) Has2FA(ctx context.Context, user string) (bool, error) {
	t, _, err := factorsOf(ctx, h.Priv, user)
	return t, mapPrivErr(err)
}

func factorsOf(ctx context.Context, p priv.Caller, user string) (totp bool, left int, err error) {
	var out struct {
		TOTP         bool `json:"totp"`
		RecoveryLeft int  `json:"recoveryLeft"`
	}
	err = p.Call(ctx, "auth.factors", priv.Meta{}, map[string]string{"username": user}, &out)
	return out.TOTP, out.RecoveryLeft, err
}

// Factors reports what the user has enrolled.
func (s *Service) Factors(ctx context.Context, user string) (totp bool, recoveryLeft int, err error) {
	totp, recoveryLeft, err = factorsOf(ctx, s.priv, user)
	return totp, recoveryLeft, mapPrivErr(err)
}

func (s *Service) newChallenge(p *pending) *Challenge {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	now := s.clk.Now()
	p.exp = now.Add(challengeTTL)
	s.ch.mu.Lock()
	defer s.ch.mu.Unlock()
	if s.ch.m == nil {
		s.ch.m = map[string]*pending{}
	}
	for k, v := range s.ch.m { // opportunistic purge
		if now.After(v.exp) {
			delete(s.ch.m, k)
		}
	}
	s.ch.m[id] = p
	return &Challenge{ID: id, Methods: p.methods, Expires: p.exp}
}

// take returns the live challenge; it is removed (single use) unless keep is true. Wrong kind/session = invalid.
func (s *Service) take(id, kind, sid string, keep bool) (*pending, error) {
	s.ch.mu.Lock()
	defer s.ch.mu.Unlock()
	p, ok := s.ch.m[id]
	if !ok || s.clk.Now().After(p.exp) || p.kind != kind || (kind == kindStepUp && p.sid != sid) {
		delete(s.ch.m, id)
		return nil, ErrInvalidChallenge
	}
	if !keep {
		delete(s.ch.m, id)
	}
	return p, nil
}

func (s *Service) burnTry(id string) {
	s.ch.mu.Lock()
	defer s.ch.mu.Unlock()
	if p, ok := s.ch.m[id]; ok {
		if p.tries++; p.tries >= challengeAttempts {
			delete(s.ch.m, id)
		}
	}
}

// checkCode verifies one factor through the helper.
func (s *Service) checkCode(ctx context.Context, p *pending, method, code string) (bool, error) {
	var out struct {
		OK bool `json:"ok"`
	}
	var rpc string
	switch method {
	case "totp":
		rpc = "auth.totp.verify"
	case "recovery":
		rpc = "auth.recovery.verify"
	default:
		return false, ErrMethodNotAllowed
	}
	err := s.priv.Call(ctx, rpc, s.meta(p.c, p.user, p.role), map[string]string{"username": p.user, "code": code}, &out)
	return out.OK, mapPrivErr(err)
}

func allowed(methods []string, m string) bool {
	for _, x := range methods {
		if x == m {
			return true
		}
	}
	return false
}

// VerifySecondFactor completes a login: the challenge is consumed on success, counted on failure.
func (s *Service) VerifySecondFactor(ctx context.Context, id, method, code string, c Client) (*LoginResult, error) {
	p, err := s.take(id, kindLogin, "", true)
	if err != nil {
		return nil, err
	}
	if p.c.UserAgent != c.UserAgent { // bound to the browser it started in; IP changes are tolerated (mobile networks)
		s.burnTry(id)
		return nil, ErrInvalidChallenge
	}
	if until, locked, e := s.throttle.AccountLocked(ctx, p.user); e == nil && locked {
		return nil, &LockedError{Until: until}
	}
	if !allowed(p.methods, method) {
		return nil, ErrMethodNotAllowed
	}
	ok, err := s.checkCode(ctx, p, method, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		s.burnTry(id)
		lockedNow, bannedNow, _ := s.throttle.Failure(ctx, p.user, c.IP, c.Origin, "bad-second-factor")
		s.failLog(p.user, c.IP, "bad-second-factor")
		s.auditEv(ctx, c, p.user, p.role, "auth.2fa.fail", "", audit.Fail, map[string]any{"method": method})
		if lockedNow {
			s.auditEv(ctx, c, p.user, "", "auth.lockout", "", audit.Denied, nil)
		}
		if bannedNow {
			s.auditEv(ctx, c, "", "", "auth.ipban", c.IP, audit.Denied, nil)
		}
		return nil, ErrInvalidCode
	}
	if _, err := s.take(id, kindLogin, "", false); err != nil { // single use: a parallel winner already took it
		return nil, err
	}
	_ = s.throttle.Success(ctx, p.user, c.IP)
	tok, sess, err := s.Sessions.Create(ctx, CreateParams{Username: p.user, UID: p.uid, Role: p.role, IP: c.IP, UserAgent: c.UserAgent,
		Origin: c.Origin, Factors: []string{"password", method}, CredEpoch: p.epoch})
	if err != nil {
		return nil, err
	}
	s.auditEv(ctx, c, p.user, p.role, "auth.login.ok", "", audit.OK, map[string]any{"origin": string(c.Origin), "method": method})
	return &LoginResult{Token: tok, Session: sess, Role: p.role}, nil
}

// ---- enrolment

// TOTPBegin starts TOTP enrolment: returns the base32 secret and the otpauth:// URI for the QR code.
func (s *Service) TOTPBegin(ctx context.Context, a *Authenticated, c Client) (secret, uri string, err error) {
	var out struct {
		Secret string `json:"secret"`
	}
	if err := s.priv.Call(ctx, "auth.totp.begin", s.meta(c, a.Session.Username, a.Role), map[string]string{"username": a.Session.Username}, &out); err != nil {
		var pe *priv.Error
		if errors.As(err, &pe) && pe.Code == priv.CodeInvalidParams {
			return "", "", ErrAlreadyEnrolled
		}
		return "", "", mapPrivErr(err)
	}
	label := url.PathEscape("Yubeos:" + a.Session.Username)
	uri = "otpauth://totp/" + label + "?secret=" + out.Secret + "&issuer=Yubeos&algorithm=SHA1&digits=6&period=30"
	s.auditEv(ctx, c, a.Session.Username, a.Role, "auth.totp.begin", "", audit.OK, nil)
	return out.Secret, uri, nil
}

// TOTPConfirm activates TOTP, returns the one-time recovery codes and, for an enrollment-only session, upgrades it.
func (s *Service) TOTPConfirm(ctx context.Context, a *Authenticated, code string, c Client) ([]string, error) {
	var out struct {
		Codes []string `json:"codes"`
	}
	user := a.Session.Username
	if err := s.priv.Call(ctx, "auth.totp.confirm", s.meta(c, user, a.Role), map[string]string{"username": user, "code": code}, &out); err != nil {
		var pe *priv.Error
		if errors.As(err, &pe) && pe.Code == priv.CodeForbidden {
			s.auditEv(ctx, c, user, a.Role, "auth.totp.confirm", "", audit.Fail, nil)
			return nil, ErrInvalidCode
		}
		if errors.As(err, &pe) && pe.Code == priv.CodeNotFound {
			return nil, ErrNotEnrolled
		}
		return nil, mapPrivErr(err)
	}
	if a.Session.EnrollmentRequired {
		if err := s.Sessions.CompleteEnrollment(ctx, a.Session.SID, append(a.Session.Factors, "totp")); err != nil {
			return nil, err
		}
	}
	s.auditEv(ctx, c, user, a.Role, "auth.totp.enabled", "", audit.OK, nil)
	return out.Codes, nil
}

// TOTPRemove disables TOTP (and drops the recovery codes). Needs a fresh step-up.
func (s *Service) TOTPRemove(ctx context.Context, a *Authenticated, c Client) error {
	if err := s.RequireStepUp(a); err != nil {
		return err
	}
	user := a.Session.Username
	if err := s.priv.Call(ctx, "auth.totp.remove", s.meta(c, user, a.Role), map[string]string{"username": user}, nil); err != nil {
		return mapPrivErr(err)
	}
	s.auditEv(ctx, c, user, a.Role, "auth.totp.removed", "", audit.OK, nil)
	return nil
}

// RegenerateRecovery replaces the recovery codes. Needs a fresh step-up.
func (s *Service) RegenerateRecovery(ctx context.Context, a *Authenticated, c Client) ([]string, error) {
	if err := s.RequireStepUp(a); err != nil {
		return nil, err
	}
	user := a.Session.Username
	var out struct {
		Codes []string `json:"codes"`
	}
	if err := s.priv.Call(ctx, "auth.recovery.regenerate", s.meta(c, user, a.Role), map[string]string{"username": user}, &out); err != nil {
		var pe *priv.Error
		if errors.As(err, &pe) && pe.Code == priv.CodeInvalidParams {
			return nil, ErrNotEnrolled
		}
		return nil, mapPrivErr(err)
	}
	s.auditEv(ctx, c, user, a.Role, "auth.recovery.regenerated", "", audit.OK, nil)
	return out.Codes, nil
}

// ---- step-up

// RequireStepUp returns ErrStepUpRequired unless the session has an open step-up window.
func (s *Service) RequireStepUp(a *Authenticated) error {
	if a.Session.ReauthUntil.After(s.clk.Now()) {
		return nil
	}
	return ErrStepUpRequired
}

// StepUpBegin lists the methods the user can answer with: a second factor when enrolled, otherwise the password.
func (s *Service) StepUpBegin(ctx context.Context, a *Authenticated, c Client) (*Challenge, error) {
	totp, _, err := s.Factors(ctx, a.Session.Username)
	if err != nil {
		return nil, err
	}
	methods := []string{"password"}
	if totp {
		methods = []string{"totp", "recovery"}
	}
	return s.newChallenge(&pending{kind: kindStepUp, user: a.Session.Username, role: a.Role, sid: a.Session.SID, c: c, methods: methods}), nil
}

// StepUp verifies the answer and opens a five-minute window on the session.
func (s *Service) StepUp(ctx context.Context, a *Authenticated, id, method, code, password string, c Client) error {
	p, err := s.take(id, kindStepUp, a.Session.SID, true)
	if err != nil {
		return err
	}
	if !allowed(p.methods, method) {
		return ErrMethodNotAllowed
	}
	var ok bool
	if method == "password" {
		var v struct {
			OK bool `json:"ok"`
		}
		if err := s.priv.Call(ctx, "auth.password.verify", s.meta(c, p.user, p.role), map[string]string{"username": p.user, "password": password}, &v); err != nil {
			return mapPrivErr(err)
		}
		ok = v.OK
	} else if ok, err = s.checkCode(ctx, p, method, code); err != nil {
		return err
	}
	if !ok {
		s.burnTry(id)
		s.throttle.Failure(ctx, p.user, c.IP, c.Origin, "bad-stepup") //nolint:errcheck
		s.auditEv(ctx, c, p.user, p.role, "auth.stepup.fail", "", audit.Fail, map[string]any{"method": strings.ToLower(method)})
		return ErrInvalidCode
	}
	if _, err := s.take(id, kindStepUp, a.Session.SID, false); err != nil {
		return err
	}
	if err := s.Sessions.OpenReauth(ctx, a.Session.SID, s.clk.Now().Add(stepUpWindow)); err != nil {
		return err
	}
	s.auditEv(ctx, c, p.user, p.role, "auth.stepup.ok", "", audit.OK, map[string]any{"method": method})
	return nil
}
