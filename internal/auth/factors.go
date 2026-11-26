package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"time"

	"github.com/serpis172/Yubeos/internal/audit"
	"github.com/serpis172/Yubeos/internal/identity"
	"github.com/serpis172/Yubeos/internal/netx"
	"github.com/serpis172/Yubeos/internal/priv"
	"github.com/serpis172/Yubeos/internal/totp"
	"github.com/serpis172/Yubeos/internal/webauthn"
)

var (
	ErrInvalidChallenge    = errors.New("auth: challenge is invalid, expired or used up")
	ErrInvalidSecondFactor = errors.New("auth: second factor rejected")
	ErrStepUpRequired      = errors.New("auth: re-authentication required")
	ErrLastFactor          = errors.New("auth: policy requires a second factor; keep at least one")
	ErrPasskeyNotSole      = errors.New("auth: passkeys may not be used alone on this server")
	ErrFlagged             = errors.New("auth: passkey flagged after a signature-counter regression")
	ErrAlreadyEnrolled     = errors.New("auth: already enrolled")
)

const (
	challengeTTL     = 5 * time.Minute
	challengeTries   = 5
	stepUpWindow     = 5 * time.Minute
	maxPasskeysPerUS = 10
)

// Challenge is returned to the browser when a second factor (or a passkey ceremony) is needed.
type Challenge struct {
	ID        string
	Methods   []string       // subset of: webauthn, totp, recovery
	WebAuthn  map[string]any // publicKey options for navigator.credentials.get()/create(); nil if not applicable
	ExpiresAt time.Time
}

type pending struct {
	kind     string // login | passkey-login | register | stepup
	user     string
	info     identity.Info
	epoch    int64
	ip       string
	uaFamily string
	created  time.Time
	tries    int
	wa       []byte
}

// Assertion is a decoded navigator.credentials.get() response.
type Assertion struct {
	RawID, AuthenticatorData, ClientDataJSON, Signature []byte
}

// Attestation is a decoded navigator.credentials.create() response.
type Attestation struct {
	AttestationObject, ClientDataJSON []byte
}

// SecondFactor is what the user presents to complete a login or a step-up.
type SecondFactor struct {
	Method    string // totp | recovery | webauthn | password (step-up only, when no factor is enrolled)
	Code      string
	Assertion *Assertion
}

// Passkey is the listing view of a credential.
type Passkey struct {
	ID         string
	Label      string
	CreatedAt  time.Time
	LastUsedAt time.Time
	Flagged    bool
}

// FactorsInfo summarises a user's second factors for the Security page.
type FactorsInfo struct {
	TOTP         bool
	Passkeys     []Passkey
	RecoveryLeft int
	Required     bool // policy demands a second factor for this user/origin
}

func (s *Service) newPending(p *pending, c Client) string {
	id := newSecret(24)
	p.created, p.ip = s.clk.Now(), c.IP
	p.uaFamily, _ = uaBits(c.UserAgent)
	s.chMu.Lock()
	defer s.chMu.Unlock()
	for k, v := range s.challenges { // opportunistic cleanup
		if s.clk.Now().Sub(v.created) > challengeTTL {
			delete(s.challenges, k)
		}
	}
	s.challenges[id] = p
	return id
}

// take returns the pending challenge if it is valid for this client; consume removes it.
func (s *Service) take(id, kind string, c Client, consume bool) (*pending, error) {
	s.chMu.Lock()
	defer s.chMu.Unlock()
	p, ok := s.challenges[id]
	if !ok || p.kind != kind || s.clk.Now().Sub(p.created) > challengeTTL || p.ip != c.IP {
		delete(s.challenges, id)
		return nil, ErrInvalidChallenge
	}
	if fam, _ := uaBits(c.UserAgent); fam != p.uaFamily {
		delete(s.challenges, id)
		return nil, ErrInvalidChallenge
	}
	if consume {
		delete(s.challenges, id)
	} else if p.tries++; p.tries > challengeTries {
		delete(s.challenges, id)
		return nil, ErrInvalidChallenge
	}
	return p, nil
}

func (s *Service) rpUser(user string) []byte {
	h := sha256.Sum256([]byte("yubeos-user:" + user))
	return h[:]
}

// ---- factor inventory --------------------------------------------------------------------------------------------

type helperFactors struct {
	TOTP         bool `json:"totp"`
	RecoveryLeft int  `json:"recoveryLeft"`
}

func (s *Service) helperFactors(ctx context.Context, user string) (helperFactors, error) {
	var f helperFactors
	err := s.priv.Call(ctx, "auth.factors.get", priv.Meta{Actor: user}, map[string]string{"username": user}, &f)
	return f, mapPrivErr(err)
}

func (s *Service) passkeyCount(ctx context.Context, user string) (int, error) {
	var n int
	err := s.Sessions.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM passkeys WHERE username=? AND flagged=0`, user).Scan(&n)
	return n, err
}

type liveFactors struct{ s *Service }

func (l liveFactors) Has2FA(ctx context.Context, user string) (bool, error) {
	n, err := l.s.passkeyCount(ctx, user)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	f, err := l.s.helperFactors(ctx, user)
	return f.TOTP, err
}

func (s *Service) methodsFor(ctx context.Context, user string) ([]string, error) {
	var m []string
	if n, err := s.passkeyCount(ctx, user); err != nil {
		return nil, err
	} else if n > 0 {
		m = append(m, "webauthn")
	}
	f, err := s.helperFactors(ctx, user)
	if err != nil {
		return nil, err
	}
	if f.TOTP {
		m = append(m, "totp")
	}
	if f.RecoveryLeft > 0 {
		m = append(m, "recovery")
	}
	return m, nil
}

func (s *Service) allowCredentials(ctx context.Context, user string) []map[string]any {
	rows, err := s.Sessions.db.QueryContext(ctx, `SELECT id FROM passkeys WHERE username=? AND flagged=0`, user)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id []byte
		if rows.Scan(&id) == nil {
			out = append(out, map[string]any{"type": "public-key", "id": webauthn.B64(id)})
		}
	}
	return out
}

func (s *Service) requestOptions(ctx context.Context, user string, chal []byte, c Client) map[string]any {
	opts := map[string]any{"challenge": webauthn.B64(chal), "rpId": c.RPID, "userVerification": "required", "timeout": 60000}
	if user != "" {
		opts["allowCredentials"] = s.allowCredentials(ctx, user)
	}
	return opts
}

// beginLogin opens the second-factor challenge after a correct password.
func (s *Service) beginLogin(ctx context.Context, user string, info identity.Info, epoch int64, c Client) (*Challenge, error) {
	methods, err := s.methodsFor(ctx, user)
	if err != nil {
		return nil, err
	}
	p := &pending{kind: "login", user: user, info: info, epoch: epoch}
	ch := &Challenge{Methods: methods} // passkey options are fetched separately (POST /auth/webauthn/login/begin)
	ch.ID = s.newPending(p, c)
	ch.ExpiresAt = p.created.Add(challengeTTL)
	return ch, nil
}

// verifyFactor checks one second-factor submission for user. It returns the method that succeeded.
func (s *Service) verifyFactor(ctx context.Context, user string, sf SecondFactor, wa []byte, c Client) (string, error) {
	switch sf.Method {
	case "totp":
		var r struct {
			OK bool `json:"ok"`
		}
		if err := s.priv.Call(ctx, "auth.totp.verify", s.meta(c, user, ""), map[string]string{"username": user, "code": sf.Code}, &r); err != nil {
			return "", mapPrivErr(err)
		}
		if r.OK {
			return "totp", nil
		}
	case "recovery":
		var r struct {
			OK   bool `json:"ok"`
			Left int  `json:"left"`
		}
		if err := s.priv.Call(ctx, "auth.recovery.verify", s.meta(c, user, ""), map[string]string{"username": user, "code": sf.Code}, &r); err != nil {
			return "", mapPrivErr(err)
		}
		if r.OK {
			_ = s.RevokeAllDevices(ctx, user, "recovery-code-used")
			s.auditEv(ctx, c, user, "", "auth.recovery.used", "", audit.OK, map[string]any{"left": r.Left})
			return "recovery", nil
		}
	case "webauthn":
		if sf.Assertion != nil && wa != nil {
			if _, err := s.checkAssertion(ctx, user, wa, sf.Assertion, c); err != nil {
				return "", err
			}
			return "passkey", nil
		}
	}
	return "", ErrInvalidSecondFactor
}

// checkAssertion verifies an assertion against a stored credential (of user, when non-empty) and updates the counter.
func (s *Service) checkAssertion(ctx context.Context, user string, chal []byte, a *Assertion, c Client) (credUser string, err error) {
	var (
		cred    webauthn.Credential
		owner   string
		count   int64
		flagged int
	)
	err = s.Sessions.db.QueryRowContext(ctx, `SELECT id, username, public_key, sign_count, flagged FROM passkeys WHERE id=?`, a.RawID).
		Scan(&cred.ID, &owner, &cred.PublicKey, &count, &flagged)
	if err != nil {
		return "", ErrInvalidSecondFactor
	}
	if user != "" && owner != user {
		return "", ErrInvalidSecondFactor
	}
	if flagged == 1 {
		return "", ErrFlagged
	}
	cred.SignCount = uint32(count)
	n, err := webauthn.VerifyAssertion(&cred, a.AuthenticatorData, a.ClientDataJSON, a.Signature, chal, c.RPOrigin, c.RPID)
	if errors.Is(err, webauthn.ErrCounter) {
		_, _ = s.Sessions.db.ExecContext(ctx, `UPDATE passkeys SET flagged=1 WHERE id=?`, a.RawID)
		s.auditEv(ctx, c, owner, "", "auth.passkey.cloned", webauthn.B64(a.RawID), audit.Denied, nil)
		s.log.Warn("passkey signature counter regression: credential flagged", "user", sanitizeLog(owner))
		return "", ErrFlagged
	}
	if err != nil {
		return "", ErrInvalidSecondFactor
	}
	_, _ = s.Sessions.db.ExecContext(ctx, `UPDATE passkeys SET sign_count=?, last_used_at=? WHERE id=?`, n, s.clk.Now().UnixMilli(), a.RawID)
	return owner, nil
}

// finishLogin creates the session after the factors are satisfied; trust adds a trusted-device cookie.
func (s *Service) finishLogin(ctx context.Context, user string, info identity.Info, epoch int64, factors []string, trust bool, deviceID string, c Client) (*LoginResult, error) {
	res := &LoginResult{Role: info.Role}
	if trust && deviceID == "" {
		tok, id, err := s.createDevice(ctx, user, info.Role, c)
		if err != nil {
			return nil, err
		}
		res.DeviceToken, deviceID = tok, id
	}
	tok, sess, err := s.Sessions.Create(ctx, CreateParams{Username: user, UID: info.UID, Role: info.Role, IP: c.IP, UserAgent: c.UserAgent,
		Origin: c.Origin, Factors: factors, CredEpoch: epoch, DeviceID: deviceID})
	if err != nil {
		return nil, err
	}
	res.Token, res.Session = tok, sess
	s.auditEv(ctx, c, user, info.Role, "auth.login.ok", "", audit.OK, map[string]any{"origin": string(c.Origin), "factors": factors})
	return res, nil
}

// CompleteLogin finishes a password login with a second factor (TOTP, recovery code or passkey).
func (s *Service) CompleteLogin(ctx context.Context, chID string, sf SecondFactor, trust bool, c Client) (*LoginResult, error) {
	p, err := s.take(chID, "login", c, false)
	if err != nil {
		return nil, err
	}
	if until, locked, _ := s.throttle.AccountLocked(ctx, p.user); locked {
		return nil, &LockedError{Until: until}
	}
	method, err := s.verifyFactor(ctx, p.user, sf, p.wa, c)
	if err != nil {
		if errors.Is(err, ErrInvalidSecondFactor) || errors.Is(err, ErrFlagged) {
			s.throttle.Failure(ctx, p.user, c.IP, c.Origin, "bad-second-factor")
			s.failLog(p.user, c.IP, "bad-second-factor")
			s.auditEv(ctx, c, p.user, p.info.Role, "auth.login.fail", "", audit.Fail, map[string]any{"reason": "bad-second-factor", "method": sf.Method})
		}
		return nil, err
	}
	_, _ = s.take(chID, "login", c, true)
	return s.finishLogin(ctx, p.user, p.info, p.epoch, []string{"password", method}, trust, "", c)
}

// PasskeyLoginBegin starts a usernameless passkey login.
func (s *Service) PasskeyLoginBegin(ctx context.Context, c Client) (*Challenge, error) {
	if !s.cfg.AllowPasskeySole {
		return nil, ErrPasskeyNotSole
	}
	p := &pending{kind: "passkey-login", wa: webauthn.NewChallenge()}
	ch := &Challenge{Methods: []string{"webauthn"}, WebAuthn: s.requestOptions(ctx, "", p.wa, c)}
	ch.ID = s.newPending(p, c)
	ch.ExpiresAt = p.created.Add(challengeTTL)
	return ch, nil
}

// PasskeyLoginFinish completes a passkey login; a verified passkey satisfies both factors.
func (s *Service) PasskeyLoginFinish(ctx context.Context, chID string, a *Assertion, trust bool, c Client) (*LoginResult, error) {
	if !s.cfg.AllowPasskeySole {
		return nil, ErrPasskeyNotSole
	}
	if c.Origin == netx.Remote {
		if until, banned, _ := s.throttle.IPBanned(ctx, c.IP); banned {
			return nil, &BannedError{Until: until}
		}
	}
	p, err := s.take(chID, "passkey-login", c, true)
	if err != nil {
		return nil, err
	}
	user, err := s.checkAssertion(ctx, "", p.wa, a, c)
	if err != nil {
		s.throttle.Failure(ctx, "", c.IP, c.Origin, "bad-passkey")
		s.failLog("", c.IP, "bad-passkey")
		return nil, err
	}
	info, ok := s.ident.Lookup(user)
	if !ok || info.Role == "" {
		return nil, ErrInvalidSecondFactor
	}
	var e struct {
		Epoch int64 `json:"epoch"`
	}
	if err := s.priv.Call(ctx, "auth.epoch.get", priv.Meta{Actor: user}, map[string]string{"username": user}, &e); err != nil {
		return nil, mapPrivErr(err)
	}
	return s.finishLogin(ctx, user, info, e.Epoch, []string{"passkey"}, trust, "", c)
}

// ---- enrolment ---------------------------------------------------------------------------------------------------

func (s *Service) afterEnroll(ctx context.Context, a *Authenticated, factor string, c Client) {
	if a.Session.EnrollmentRequired {
		_ = s.Sessions.ClearEnrollment(ctx, a.Session.SID, factor)
	}
	_, _ = s.Sessions.db.ExecContext(ctx, `UPDATE users_meta SET has_2fa=1 WHERE username=?`, a.Session.Username)
	s.auditEv(ctx, c, a.Session.Username, a.Role, "auth.factor.enrol", factor, audit.OK, nil)
}

// TOTPBegin creates a pending TOTP secret and returns it with its otpauth:// URI.
func (s *Service) TOTPBegin(ctx context.Context, a *Authenticated, c Client) (secret, uri string, err error) {
	user := a.Session.Username
	var r struct {
		Secret string `json:"secret"`
	}
	if err := s.priv.Call(ctx, "auth.totp.begin", s.meta(c, user, a.Role), map[string]string{"username": user}, &r); err != nil {
		var pe *priv.Error
		if errors.As(err, &pe) && pe.Code == priv.CodeInvalidParams {
			return "", "", ErrAlreadyEnrolled
		}
		return "", "", mapPrivErr(err)
	}
	raw, _ := totp.DecodeSecret(r.Secret)
	issuer := s.cfg.Issuer
	if issuer == "" {
		issuer = "Yubeos"
	}
	return r.Secret, totp.URI(issuer, user, raw), nil
}

// TOTPConfirm activates TOTP after a valid code. It returns recovery codes the first time (shown once).
func (s *Service) TOTPConfirm(ctx context.Context, a *Authenticated, code string, c Client) ([]string, error) {
	user := a.Session.Username
	var r struct {
		OK    bool     `json:"ok"`
		Codes []string `json:"codes"`
	}
	if err := s.priv.Call(ctx, "auth.totp.confirm", s.meta(c, user, a.Role), map[string]string{"username": user, "code": code}, &r); err != nil {
		return nil, mapPrivErr(err)
	}
	if !r.OK {
		return nil, ErrInvalidSecondFactor
	}
	s.afterEnroll(ctx, a, "totp", c)
	return r.Codes, nil
}

func (s *Service) mustKeepFactor(ctx context.Context, a *Authenticated, removing string) error {
	if !NeedsSecondFactor(s.cfg.Require2FA, a.Role, a.Session.Origin) {
		return nil
	}
	n, _ := s.passkeyCount(ctx, a.Session.Username)
	f, _ := s.helperFactors(ctx, a.Session.Username)
	left := n
	if f.TOTP {
		left++
	}
	if left <= 1 && (removing == "totp" && f.TOTP || removing == "passkey" && n > 0) {
		return ErrLastFactor
	}
	return nil
}

// TOTPRemove disables TOTP (step-up required by the caller).
func (s *Service) TOTPRemove(ctx context.Context, a *Authenticated, c Client) error {
	if err := s.mustKeepFactor(ctx, a, "totp"); err != nil {
		return err
	}
	user := a.Session.Username
	if err := s.priv.Call(ctx, "auth.totp.remove", s.meta(c, user, a.Role), map[string]string{"username": user}, nil); err != nil {
		return mapPrivErr(err)
	}
	_ = s.RevokeAllDevices(ctx, user, "2fa-removed")
	s.auditEv(ctx, c, user, a.Role, "auth.factor.remove", "totp", audit.OK, nil)
	return nil
}

// RecoveryRegenerate replaces the recovery codes (step-up required by the caller).
func (s *Service) RecoveryRegenerate(ctx context.Context, a *Authenticated, c Client) ([]string, error) {
	user := a.Session.Username
	var r struct {
		Codes []string `json:"codes"`
	}
	if err := s.priv.Call(ctx, "auth.recovery.regenerate", s.meta(c, user, a.Role), map[string]string{"username": user}, &r); err != nil {
		return nil, mapPrivErr(err)
	}
	s.auditEv(ctx, c, user, a.Role, "auth.recovery.regenerate", "", audit.OK, nil)
	return r.Codes, nil
}

// PasskeyRegisterBegin returns the navigator.credentials.create() options.
func (s *Service) PasskeyRegisterBegin(ctx context.Context, a *Authenticated, c Client) (*Challenge, error) {
	user := a.Session.Username
	if n, _ := s.passkeyCount(ctx, user); n >= maxPasskeysPerUS {
		return nil, errors.New("auth: too many passkeys")
	}
	p := &pending{kind: "register", user: user, wa: webauthn.NewChallenge()}
	opts := map[string]any{
		"challenge":              webauthn.B64(p.wa),
		"rp":                     map[string]any{"id": c.RPID, "name": "Yubeos"},
		"user":                   map[string]any{"id": webauthn.B64(s.rpUser(user)), "name": user, "displayName": user},
		"pubKeyCredParams":       []map[string]any{{"type": "public-key", "alg": -7}},
		"authenticatorSelection": map[string]any{"residentKey": "preferred", "userVerification": "required"},
		"attestation":            "none",
		"excludeCredentials":     s.allowCredentials(ctx, user),
		"timeout":                60000,
	}
	ch := &Challenge{WebAuthn: opts}
	ch.ID = s.newPending(p, c)
	ch.ExpiresAt = p.created.Add(challengeTTL)
	return ch, nil
}

// PasskeyRegisterFinish verifies the attestation and stores the credential.
func (s *Service) PasskeyRegisterFinish(ctx context.Context, a *Authenticated, chID, label string, att *Attestation, c Client) (*Passkey, error) {
	p, err := s.take(chID, "register", c, true)
	if err != nil || p.user != a.Session.Username {
		return nil, ErrInvalidChallenge
	}
	cred, err := webauthn.ParseRegistration(att.AttestationObject, att.ClientDataJSON, p.wa, c.RPOrigin, c.RPID)
	if err != nil {
		return nil, err
	}
	if label == "" || len(label) > 60 {
		label = "Passkey " + time.Now().Format("2006-01-02")
	}
	now := s.clk.Now().UnixMilli()
	if _, err := s.Sessions.db.ExecContext(ctx, `INSERT INTO passkeys(id, username, public_key, sign_count, label, rp_id, created_at) VALUES(?,?,?,?,?,?,?)`,
		cred.ID, p.user, cred.PublicKey, cred.SignCount, label, c.RPID, now); err != nil {
		return nil, err
	}
	s.afterEnroll(ctx, a, "passkey", c)
	return &Passkey{ID: webauthn.B64(cred.ID), Label: label, CreatedAt: time.UnixMilli(now)}, nil
}

// ListPasskeys lists a user's passkeys.
func (s *Service) ListPasskeys(ctx context.Context, user string) ([]Passkey, error) {
	rows, err := s.Sessions.db.QueryContext(ctx, `SELECT id, label, created_at, last_used_at, flagged FROM passkeys WHERE username=? ORDER BY created_at`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Passkey{}
	for rows.Next() {
		var (
			id      []byte
			p       Passkey
			cr      int64
			used    sql.NullInt64
			flagged int
		)
		if err := rows.Scan(&id, &p.Label, &cr, &used, &flagged); err != nil {
			return nil, err
		}
		p.ID, p.CreatedAt, p.Flagged = webauthn.B64(id), time.UnixMilli(cr), flagged == 1
		if used.Valid {
			p.LastUsedAt = time.UnixMilli(used.Int64)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RemovePasskey deletes a passkey (step-up required by the caller).
func (s *Service) RemovePasskey(ctx context.Context, a *Authenticated, id string, c Client) (bool, error) {
	raw, err := webauthn.FromB64(id)
	if err != nil {
		return false, nil
	}
	if err := s.mustKeepFactor(ctx, a, "passkey"); err != nil {
		return false, err
	}
	res, err := s.Sessions.db.ExecContext(ctx, `DELETE FROM passkeys WHERE id=? AND username=?`, raw, a.Session.Username)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		_ = s.RevokeAllDevices(ctx, a.Session.Username, "2fa-removed")
		s.auditEv(ctx, c, a.Session.Username, a.Role, "auth.factor.remove", "passkey", audit.OK, nil)
	}
	return n > 0, nil
}

// Factors returns the factor overview for the Security page.
func (s *Service) Factors(ctx context.Context, a *Authenticated) (*FactorsInfo, error) {
	user := a.Session.Username
	pk, err := s.ListPasskeys(ctx, user)
	if err != nil {
		return nil, err
	}
	f, err := s.helperFactors(ctx, user)
	if err != nil {
		return nil, err
	}
	return &FactorsInfo{TOTP: f.TOTP, Passkeys: pk, RecoveryLeft: f.RecoveryLeft, Required: NeedsSecondFactor(s.cfg.Require2FA, a.Role, a.Session.Origin)}, nil
}

// ---- step-up -----------------------------------------------------------------------------------------------------

// StepUpBegin returns the methods available for re-authentication (and passkey options when one exists).
func (s *Service) StepUpBegin(ctx context.Context, a *Authenticated, c Client) (*Challenge, error) {
	user := a.Session.Username
	methods, err := s.methodsFor(ctx, user)
	if err != nil {
		return nil, err
	}
	if len(methods) == 0 {
		methods = []string{"password"}
	}
	p := &pending{kind: "stepup", user: user}
	ch := &Challenge{Methods: methods}
	for _, m := range methods {
		if m == "webauthn" {
			p.wa = webauthn.NewChallenge()
			ch.WebAuthn = s.requestOptions(ctx, user, p.wa, c)
		}
	}
	ch.ID = s.newPending(p, c)
	ch.ExpiresAt = p.created.Add(challengeTTL)
	return ch, nil
}

// StepUp verifies a factor and opens a short re-authentication window on the session. A trusted device can never
// satisfy it. With no enrolled factor, the password is accepted.
func (s *Service) StepUp(ctx context.Context, a *Authenticated, chID, password string, sf SecondFactor, c Client) error {
	user := a.Session.Username
	if sf.Method == "password" {
		methods, _ := s.methodsFor(ctx, user)
		if len(methods) > 0 {
			return ErrInvalidSecondFactor
		}
		var v struct {
			OK bool `json:"ok"`
		}
		if err := s.priv.Call(ctx, "auth.password.verify", s.meta(c, user, a.Role), map[string]string{"username": user, "password": password}, &v); err != nil {
			return mapPrivErr(err)
		}
		if !v.OK {
			s.throttle.Failure(ctx, user, c.IP, c.Origin, "bad-stepup")
			return ErrInvalidSecondFactor
		}
	} else {
		p, err := s.take(chID, "stepup", c, false)
		if err != nil || p.user != user {
			return ErrInvalidChallenge
		}
		if _, err := s.verifyFactor(ctx, user, sf, p.wa, c); err != nil {
			s.throttle.Failure(ctx, user, c.IP, c.Origin, "bad-stepup")
			s.failLog(user, c.IP, "bad-stepup")
			return err
		}
		_, _ = s.take(chID, "stepup", c, true)
	}
	s.auditEv(ctx, c, user, a.Role, "auth.stepup", "", audit.OK, nil)
	return s.Sessions.SetReauth(ctx, a.Session.SID, s.clk.Now().Add(stepUpWindow))
}

// HasStepUp reports whether the session re-authenticated recently.
func (s *Service) HasStepUp(a *Session) bool { return a.ReauthUntil.After(s.clk.Now()) }

// WebAuthnLoginBegin returns assertion options. With chID (the id from the 202 login answer) the options are bound
// to that user's passkeys; without it a usernameless passkey login starts.
func (s *Service) WebAuthnLoginBegin(ctx context.Context, chID string, c Client) (*Challenge, error) {
	if chID == "" {
		return s.PasskeyLoginBegin(ctx, c)
	}
	p, err := s.take(chID, "login", c, false)
	if err != nil {
		return nil, err
	}
	wa := webauthn.NewChallenge()
	s.chMu.Lock()
	p.wa = wa
	s.chMu.Unlock()
	return &Challenge{ID: chID, Methods: []string{"webauthn"}, WebAuthn: s.requestOptions(ctx, p.user, wa, c), ExpiresAt: p.created.Add(challengeTTL)}, nil
}

// WebAuthnLoginFinish completes either flavour: a passkey as second factor after the password, or a passkey alone.
func (s *Service) WebAuthnLoginFinish(ctx context.Context, chID string, a *Assertion, trust bool, c Client) (*LoginResult, error) {
	s.chMu.Lock()
	p, ok := s.challenges[chID]
	kind := ""
	if ok {
		kind = p.kind
	}
	s.chMu.Unlock()
	if kind == "login" {
		return s.CompleteLogin(ctx, chID, SecondFactor{Method: "webauthn", Assertion: a}, trust, c)
	}
	return s.PasskeyLoginFinish(ctx, chID, a, trust, c)
}
