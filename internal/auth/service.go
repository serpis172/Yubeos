package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/serpis172/Yubeos/internal/audit"
	"github.com/serpis172/Yubeos/internal/clock"
	"github.com/serpis172/Yubeos/internal/identity"
	"github.com/serpis172/Yubeos/internal/netx"
	"github.com/serpis172/Yubeos/internal/priv"
)

// Errors returned by the service; the HTTP layer maps them to problem types.
var (
	ErrInvalidCredentials      = errors.New("auth: invalid credentials")
	ErrEnrollmentRequiredLocal = errors.New("auth: second factor required but not enrolled; enrol from a local network")
	ErrSecondFactorUnavailable = errors.New("auth: second-factor verification is not available in this build")
	ErrPrivUnavailable         = errors.New("auth: privileged helper unavailable")
	ErrNotLocal                = errors.New("auth: this action is only allowed from a local network")
	ErrInvalidSetupToken       = errors.New("auth: setup link is invalid or expired")
)

// LockedError: password logins for the account are temporarily refused.
type LockedError struct{ Until time.Time }

func (e *LockedError) Error() string { return "auth: account temporarily locked" }

// BannedError: the client address is temporarily banned.
type BannedError struct{ Until time.Time }

func (e *BannedError) Error() string { return "auth: address temporarily banned" }

// WeakPasswordError carries the policy message for the UI.
type WeakPasswordError struct{ Msg string }

func (e *WeakPasswordError) Error() string { return e.Msg }

// Client describes the caller of an auth operation.
type Client struct {
	IP        string
	Origin    netx.Origin
	UserAgent string
	ReqID     string
}

// FactorStatus tells whether a user has a second factor enrolled (implemented with TOTP/passkeys in Phase 1c).
type FactorStatus interface {
	Has2FA(ctx context.Context, username string) (bool, error)
}

type noFactors struct{}

func (noFactors) Has2FA(context.Context, string) (bool, error) { return false, nil }

// Config of the service.
type Config struct {
	Require2FA string // none | admins | all
}

// Service orchestrates login, sessions, throttling, audit and the helper.
type Service struct {
	cfg      Config
	priv     priv.Caller
	ident    identity.Resolver
	Sessions *Sessions
	throttle *Throttle
	audit    *audit.Log
	clk      clock.Clock
	log      *slog.Logger
	factors  FactorStatus
	ch       challenges

	epochMu    sync.Mutex
	epochCheck map[string]time.Time // sid → last epoch verification
}

type Deps struct {
	Config   Config
	Priv     priv.Caller
	Identity identity.Resolver
	Sessions *Sessions
	Throttle *Throttle
	Audit    *audit.Log
	Clock    clock.Clock
	Log      *slog.Logger
	Factors  FactorStatus
}

func NewService(d Deps) *Service {
	s := &Service{cfg: d.Config, priv: d.Priv, ident: d.Identity, Sessions: d.Sessions, throttle: d.Throttle, audit: d.Audit,
		clk: d.Clock, log: d.Log, factors: d.Factors, epochCheck: map[string]time.Time{}}
	if s.clk == nil {
		s.clk = clock.Real{}
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.factors == nil {
		s.factors = noFactors{}
	}
	return s
}

func (s *Service) meta(c Client, actor string, role identity.Role) priv.Meta {
	return priv.Meta{ReqID: c.ReqID, Actor: actor, Role: string(role), IP: c.IP}
}

func (s *Service) auditEv(ctx context.Context, c Client, actor string, role identity.Role, action, target, result string, params map[string]any) {
	_ = s.audit.Append(ctx, audit.Entry{Actor: actor, Role: string(role), IP: c.IP, ReqID: c.ReqID, Action: action, Target: target, Result: result, Params: params})
}

// failLog writes the line the packaged fail2ban filter matches (docs/AUTHENTICATION.md §8).
func (s *Service) failLog(user, ip, reason string) {
	s.log.Warn(fmt.Sprintf("yubeos-auth fail user=%s ip=%s reason=%s", sanitizeLog(user), ip, reason))
}

func sanitizeLog(s string) string {
	s = normUser(s)
	var b strings.Builder
	for _, r := range s {
		if r > 32 && r < 127 && r != '=' {
			b.WriteRune(r)
		} else {
			b.WriteRune('?')
		}
	}
	return b.String()
}

func mapPrivErr(err error) error {
	if err == nil {
		return nil
	}
	var pe *priv.Error
	if errors.As(err, &pe) {
		switch pe.Code {
		case priv.CodeUnavailable, priv.CodeTimeout:
			return ErrPrivUnavailable
		case priv.CodeWeakPassword:
			return &WeakPasswordError{Msg: pe.Message}
		case priv.CodeInvalidToken:
			return ErrInvalidSetupToken
		case priv.CodeForbidden:
			return ErrInvalidCredentials
		}
	}
	return err
}

// LoginResult is returned by a successful Login.
type LoginResult struct {
	Token   string // cookie value; empty when Challenge is set
	Session Session
	Role    identity.Role
	// Challenge is set when the password was right but a second factor is still needed (HTTP 202).
	Challenge *Challenge
}

// Login verifies the password through the helper and applies lockout, ban and second-factor policy.
func (s *Service) Login(ctx context.Context, username, password string, c Client) (*LoginResult, error) {
	user := normUser(username)

	if c.Origin == netx.Remote {
		if until, banned, err := s.throttle.IPBanned(ctx, c.IP); err == nil && banned {
			s.auditEv(ctx, c, user, "", "auth.login.denied", "", audit.Denied, map[string]any{"reason": "ip-banned"})
			return nil, &BannedError{Until: until}
		}
	}
	if until, locked, err := s.throttle.AccountLocked(ctx, user); err == nil && locked {
		s.auditEv(ctx, c, user, "", "auth.login.denied", "", audit.Denied, map[string]any{"reason": "locked-out"})
		return nil, &LockedError{Until: until}
	}

	var v struct {
		OK    bool  `json:"ok"`
		Epoch int64 `json:"epoch"`
	}
	if err := s.priv.Call(ctx, "auth.password.verify", s.meta(c, user, ""), map[string]string{"username": user, "password": password}, &v); err != nil {
		return nil, mapPrivErr(err)
	}
	info, exists := s.ident.Lookup(user)
	reason := ""
	switch {
	case !v.OK:
		reason = "bad-credentials"
	case !exists:
		reason = "no-linux-account"
	case info.Role == "":
		reason = "no-role"
	}
	if reason != "" {
		lockedNow, bannedNow, _ := s.throttle.Failure(ctx, user, c.IP, c.Origin, reason)
		s.failLog(user, c.IP, reason)
		s.auditEv(ctx, c, user, "", "auth.login.fail", "", audit.Fail, map[string]any{"reason": reason})
		if lockedNow {
			s.auditEv(ctx, c, user, "", "auth.lockout", "", audit.Denied, nil)
		}
		if bannedNow {
			s.auditEv(ctx, c, "", "", "auth.ipban", c.IP, audit.Denied, nil)
		}
		return nil, ErrInvalidCredentials // identical for every reason: no enumeration
	}

	// Password is correct from here on.
	_ = s.throttle.Success(ctx, user, c.IP)
	has, err := s.factors.Has2FA(ctx, user)
	if err != nil {
		return nil, err
	}
	// Policy decides who MUST have a second factor; anyone who enrolled one is always asked for it.
	need := NeedsSecondFactor(s.cfg.Require2FA, info.Role, c.Origin) || has
	enrollOnly := false
	switch {
	case need && has:
		ch := s.newChallenge(&pending{kind: kindLogin, user: user, uid: info.UID, role: info.Role, epoch: v.Epoch, c: c,
			methods: []string{"totp", "recovery"}})
		s.auditEv(ctx, c, user, info.Role, "auth.login.2fa-required", "", audit.OK, nil)
		return &LoginResult{Challenge: ch, Role: info.Role}, nil
	case need && !has && c.Origin == netx.Remote:
		s.auditEv(ctx, c, user, info.Role, "auth.login.denied", "", audit.Denied, map[string]any{"reason": "enrollment-required-local"})
		return nil, ErrEnrollmentRequiredLocal
	case need && !has:
		enrollOnly = true // local origin: restricted session that can only enrol a second factor
	}

	tok, sess, err := s.Sessions.Create(ctx, CreateParams{Username: user, UID: info.UID, Role: info.Role, IP: c.IP, UserAgent: c.UserAgent,
		Origin: c.Origin, Factors: []string{"password"}, EnrollmentRequired: enrollOnly, CredEpoch: v.Epoch})
	if err != nil {
		return nil, err
	}
	s.auditEv(ctx, c, user, info.Role, "auth.login.ok", "", audit.OK, map[string]any{"origin": string(c.Origin), "enrollmentRequired": enrollOnly})
	return &LoginResult{Token: tok, Session: sess, Role: info.Role}, nil
}

// Authenticated is the result of validating a session cookie.
type Authenticated struct {
	Session  Session
	Role     identity.Role
	NewToken string // non-empty: the cookie value rotated; send it to the browser
}

const (
	identityRecheck = 60 * time.Second
)

// Authenticate validates a cookie, re-checks the Linux account/role and the credential epoch (≤ every 60 s).
func (s *Service) Authenticate(ctx context.Context, token string) (*Authenticated, error) {
	sess, newTok, err := s.Sessions.Lookup(ctx, token)
	if err != nil {
		return nil, err
	}
	info, ok := s.ident.Lookup(sess.Username)
	if !ok || info.Role == "" {
		_, _ = s.Sessions.RevokeSID(ctx, sess.Username, sess.SID)
		return nil, ErrNoSession
	}
	now := s.clk.Now()
	s.epochMu.Lock()
	last, seen := s.epochCheck[sess.SID]
	due := !seen || now.Sub(last) >= identityRecheck
	if due {
		s.epochCheck[sess.SID] = now
	}
	s.epochMu.Unlock()
	if due {
		var e struct {
			Epoch int64 `json:"epoch"`
		}
		if err := s.priv.Call(ctx, "auth.epoch.get", priv.Meta{Actor: sess.Username}, map[string]string{"username": sess.Username}, &e); err == nil {
			if e.Epoch != sess.CredEpoch {
				_, _ = s.Sessions.RevokeSID(ctx, sess.Username, sess.SID)
				return nil, ErrNoSession
			}
		} else {
			s.log.Warn("credential epoch check skipped: helper unavailable", "err", err) // availability over strictness, logged
		}
	}
	return &Authenticated{Session: *sess, Role: info.Role, NewToken: newTok}, nil
}

// Logout revokes the session behind token.
func (s *Service) Logout(ctx context.Context, a *Authenticated, token string, c Client) {
	_ = s.Sessions.Revoke(ctx, token)
	s.auditEv(ctx, c, a.Session.Username, a.Role, "auth.logout", "", audit.OK, nil)
}

// Setup consumes a one-time setup link (local origin only) and sets the first password.
func (s *Service) Setup(ctx context.Context, token, newPassword string, c Client) error {
	if c.Origin != netx.Local {
		s.auditEv(ctx, c, "", "", "auth.setup-link.consume", "", audit.Denied, map[string]any{"reason": "remote-origin"})
		return ErrNotLocal
	}
	if _, banned, _ := s.throttle.IPBanned(ctx, c.IP); banned {
		return &BannedError{}
	}
	var out struct {
		Username string `json:"username"`
	}
	err := s.priv.Call(ctx, "auth.setup.consume", s.meta(c, "", ""), map[string]string{"token": token, "newPassword": newPassword}, &out)
	if err != nil {
		err = mapPrivErr(err)
		if errors.Is(err, ErrInvalidSetupToken) {
			s.failLog("", c.IP, "bad-setup-token")
			s.auditEv(ctx, c, "", "", "auth.setup-link.consume", "", audit.Fail, nil)
		}
		return err
	}
	_, _ = s.Sessions.RevokeUser(ctx, out.Username, "")
	s.auditEv(ctx, c, out.Username, "", "auth.setup-link.consume", out.Username, audit.OK, nil)
	return nil
}

// ChangePassword verifies the current password in the helper, sets the new one and revokes the user's other sessions.
// (Step-up with a second factor is added in Phase 1c; until then the current password is the proof.)
func (s *Service) ChangePassword(ctx context.Context, a *Authenticated, current, next string, c Client) error {
	user := a.Session.Username
	if until, locked, _ := s.throttle.AccountLocked(ctx, user); locked {
		return &LockedError{Until: until}
	}
	err := s.priv.Call(ctx, "auth.password.change", s.meta(c, user, a.Role),
		map[string]string{"username": user, "currentPassword": current, "newPassword": next}, nil)
	if err != nil {
		err = mapPrivErr(err)
		if errors.Is(err, ErrInvalidCredentials) {
			_, _, _ = s.throttle.Failure(ctx, user, c.IP, c.Origin, "bad-current-password")
			s.failLog(user, c.IP, "bad-current-password")
			s.auditEv(ctx, c, user, a.Role, "auth.password.change", "", audit.Fail, nil)
		}
		return err
	}
	_ = s.throttle.Success(ctx, user, c.IP)
	_, _ = s.Sessions.RevokeUser(ctx, user, a.Session.SID)
	// The helper bumped the credential epoch; our own session survives, so adopt the new epoch.
	var e struct {
		Epoch int64 `json:"epoch"`
	}
	if err := s.priv.Call(ctx, "auth.epoch.get", priv.Meta{Actor: user}, map[string]string{"username": user}, &e); err == nil {
		_ = s.Sessions.SetEpoch(ctx, a.Session.SID, e.Epoch)
	}
	s.epochMu.Lock()
	delete(s.epochCheck, a.Session.SID)
	s.epochMu.Unlock()
	s.auditEv(ctx, c, user, a.Role, "auth.password.change", "", audit.OK, nil)
	return nil
}

// Purge removes expired sessions and old throttling records; also forgets epoch-check bookkeeping of dead sessions.
func (s *Service) Purge(ctx context.Context) error {
	if err := s.Sessions.Purge(ctx); err != nil {
		return err
	}
	if err := s.throttle.Purge(ctx); err != nil {
		return err
	}
	s.epochMu.Lock()
	cut := s.clk.Now().Add(-time.Hour)
	for sid, at := range s.epochCheck {
		if at.Before(cut) {
			delete(s.epochCheck, sid)
		}
	}
	s.epochMu.Unlock()
	return nil
}
