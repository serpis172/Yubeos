package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/serpis172/Yubeos/internal/audit"
	"github.com/serpis172/Yubeos/internal/clock"
	"github.com/serpis172/Yubeos/internal/db"
	"github.com/serpis172/Yubeos/internal/identity"
	"github.com/serpis172/Yubeos/internal/netx"
	"github.com/serpis172/Yubeos/internal/priv"
	"github.com/serpis172/Yubeos/internal/privd"
	"github.com/serpis172/Yubeos/internal/privd/credstore"
)

const pw = "correct-horse-battery-staple"

type env struct {
	svc   *Service
	clk   *clock.Fake
	db    *db.DB
	store *credstore.Store
	root  priv.Caller
	audit *audit.Log
}

func newEnv(t *testing.T, require2fa string, idents identity.Static) *env {
	t.Helper()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	clk := clock.NewFake(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "y.db"), db.Migrations(), quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	fast := credstore.Params{M: 1024, T: 1, P: 1}
	st, err := credstore.Open(context.Background(), t.TempDir(), credstore.Options{Params: &fast, Clock: clk, Log: quiet})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := privd.NewServer(quiet)
	credstore.Register(srv, st)
	al := audit.New(d, clk, quiet)
	svc := NewService(Deps{
		Config:   Config{Require2FA: require2fa},
		Priv:     privd.Direct{Server: srv, UID: 998}, // the daemon's uid
		Identity: idents,
		Sessions: NewSessions(d, clk, SessionConfig{IdleLocal: 30 * time.Minute, IdleRemote: 15 * time.Minute, MaxAge: 12 * time.Hour}),
		Throttle: NewThrottle(d, clk, ThrottleConfig{}),
		Audit:    al, Clock: clk, Log: quiet,
	})
	return &env{svc: svc, clk: clk, db: d, store: st, root: privd.Direct{Server: srv, UID: 0}, audit: al}
}

func (e *env) enrol(t *testing.T, user, password string) {
	t.Helper()
	var tok struct{ Token string }
	if err := e.root.Call(context.Background(), "auth.setup.create", priv.Meta{}, map[string]any{"username": user, "purpose": "first-password"}, &tok); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Setup(context.Background(), tok.Token, password, local); err != nil {
		t.Fatal(err)
	}
}

var (
	local  = Client{IP: "192.168.1.20", Origin: netx.Local, UserAgent: "test"}
	remote = Client{IP: "203.0.113.50", Origin: netx.Remote, UserAgent: "test"}
)

func idents() identity.Static {
	return identity.Static{
		"alice": {Username: "alice", UID: 1000, Role: identity.Admin},
		"bob":   {Username: "bob", UID: 1001, Role: identity.User},
		"nogrp": {Username: "nogrp", UID: 1002, Role: ""},
	}
}

func TestLoginSuccessLocalUserWithoutPolicy(t *testing.T) {
	e := newEnv(t, "none", idents())
	e.enrol(t, "bob", pw)
	r, err := e.svc.Login(context.Background(), "Bob", pw, local)
	if err != nil {
		t.Fatal(err)
	}
	if r.Role != identity.User || r.Session.EnrollmentRequired || r.Token == "" || r.Session.CSRF == "" {
		t.Fatalf("unexpected result %+v", r)
	}
	a, err := e.svc.Authenticate(context.Background(), r.Token)
	if err != nil || a.Session.Username != "bob" || a.Role != identity.User {
		t.Fatalf("authenticate: %v %+v", err, a)
	}
	if n, err := e.audit.Verify(context.Background()); err != nil || n == 0 {
		t.Fatalf("audit chain: n=%d err=%v", n, err)
	}
}

func TestFailuresAreIndistinguishable(t *testing.T) {
	e := newEnv(t, "none", idents())
	e.enrol(t, "bob", pw)
	e.enrol(t, "nogrp", pw)
	cases := map[string]struct{ u, p string }{
		"wrong password":       {"bob", "wrong-password-xyz"},
		"unknown user":         {"ghost", pw},
		"no linux role":        {"nogrp", pw},
		"weird name":           {"bob\x00; DROP", pw},
		"empty":                {"", ""},
		"credentials w/o unix": {"vanished", pw},
	}
	e.enrol(t, "vanished", pw) // has credentials but no Linux account
	for name, tc := range cases {
		_, err := e.svc.Login(context.Background(), tc.u, tc.p, local)
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("%s: got %v, want the uniform ErrInvalidCredentials", name, err)
		}
	}
}

func TestLockoutAfterRepeatedFailuresAndRecovery(t *testing.T) {
	e := newEnv(t, "none", idents())
	e.enrol(t, "bob", pw)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_, _ = e.svc.Login(ctx, "bob", "wrong-password-"+string(rune('a'+i)), local)
	}
	_, err := e.svc.Login(ctx, "bob", pw, local) // even the CORRECT password is refused while locked
	var le *LockedError
	if !errors.As(err, &le) {
		t.Fatalf("expected lockout, got %v", err)
	}
	// lockout applies to unknown names too (no enumeration)
	for i := 0; i < 10; i++ {
		_, _ = e.svc.Login(ctx, "ghost", "x", local)
	}
	if _, err := e.svc.Login(ctx, "ghost", "x", local); !errors.As(err, &le) {
		t.Fatalf("unknown accounts must lock identically, got %v", err)
	}
	e.clk.Advance(16 * time.Minute)
	if _, err := e.svc.Login(ctx, "bob", pw, local); err != nil {
		t.Fatalf("login must work again after the lockout expires: %v", err)
	}
	// and the streak is reset by the success
	if _, err := e.svc.Login(ctx, "bob", "wrong-again-123", local); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("one failure after success must not lock: %v", err)
	}
}

func TestIPBanForRemoteOriginsOnlyAndEscalation(t *testing.T) {
	e := newEnv(t, "all", idents())
	e.enrol(t, "bob", pw)
	ctx := context.Background()
	for i := 0; i < 25; i++ {
		_, _ = e.svc.Login(ctx, "user"+string(rune('a'+i)), "x", remote) // distinct names: no account lockout
	}
	_, err := e.svc.Login(ctx, "bob", pw, remote)
	var be *BannedError
	if !errors.As(err, &be) {
		t.Fatalf("expected IP ban, got %v", err)
	}
	if be.Until.Sub(e.clk.Now()) < 59*time.Minute {
		t.Fatalf("first ban should last ~1 h, got %v", be.Until.Sub(e.clk.Now()))
	}
	// local origins are never banned
	for i := 0; i < 25; i++ {
		_, _ = e.svc.Login(ctx, "x"+string(rune('a'+i)), "x", local)
	}
	if _, err := e.svc.Login(ctx, "bob", pw, local); errors.As(err, &be) {
		t.Fatal("local origins must never be IP-banned")
	}
	// second offence after the first ban expired doubles the duration
	e.clk.Advance(61 * time.Minute)
	for i := 0; i < 25; i++ {
		_, _ = e.svc.Login(ctx, "again"+string(rune('a'+i)), "x", remote)
	}
	_, err = e.svc.Login(ctx, "bob", pw, remote)
	if !errors.As(err, &be) || be.Until.Sub(e.clk.Now()) < 119*time.Minute {
		t.Fatalf("second ban should be ~2 h, got %v (%v)", err, be)
	}
}

func TestSecondFactorPolicyOutcomes(t *testing.T) {
	// require2fa=admins: local admin gets an enrollment-only session; local user a normal one
	e := newEnv(t, "admins", idents())
	e.enrol(t, "alice", pw)
	e.enrol(t, "bob", pw)
	ctx := context.Background()
	r, err := e.svc.Login(ctx, "alice", pw, local)
	if err != nil || !r.Session.EnrollmentRequired {
		t.Fatalf("local admin without 2FA must get an enrollment-only session: %v %+v", err, r)
	}
	if r, err = e.svc.Login(ctx, "bob", pw, local); err != nil || r.Session.EnrollmentRequired {
		t.Fatalf("local plain user needs no 2FA under 'admins': %v %+v", err, r)
	}
	// remote origins: always refused until a second factor exists, even for plain users and policy none
	e2 := newEnv(t, "none", idents())
	e2.enrol(t, "bob", pw)
	if _, err := e2.svc.Login(ctx, "bob", pw, remote); !errors.Is(err, ErrEnrollmentRequiredLocal) {
		t.Fatalf("remote login without 2FA must be refused, got %v", err)
	}
	if got := NeedsSecondFactor("none", identity.User, netx.Remote); !got {
		t.Fatal("remote origin must need a second factor whatever the setting")
	}
	if NeedsSecondFactor("none", identity.Admin, netx.Local) || !NeedsSecondFactor("all", identity.User, netx.Local) ||
		!NeedsSecondFactor("admins", identity.Admin, netx.Local) || NeedsSecondFactor("admins", identity.User, netx.Local) {
		t.Fatal("policy matrix wrong")
	}
}

func TestSessionLifecycle(t *testing.T) {
	e := newEnv(t, "none", idents())
	e.enrol(t, "bob", pw)
	ctx := context.Background()
	r, _ := e.svc.Login(ctx, "bob", pw, local)

	// idle timeout (local 30 min)
	e.clk.Advance(29 * time.Minute)
	if _, err := e.svc.Authenticate(ctx, r.Token); err != nil {
		t.Fatalf("within idle window: %v", err)
	}
	e.clk.Advance(31 * time.Minute)
	if _, err := e.svc.Authenticate(ctx, r.Token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("idle session must expire: %v", err)
	}

	// rotation: after 15 min the cookie value changes; the old one works for a short grace only
	r, _ = e.svc.Login(ctx, "bob", pw, local)
	e.clk.Advance(16 * time.Minute)
	a, err := e.svc.Authenticate(ctx, r.Token)
	if err != nil || a.NewToken == "" {
		t.Fatalf("expected a rotated token: %v %+v", err, a)
	}
	if _, err := e.svc.Authenticate(ctx, r.Token); err != nil { // parallel request with the old value
		t.Fatalf("old token must work within the grace period: %v", err)
	}
	e.clk.Advance(31 * time.Second)
	if _, err := e.svc.Authenticate(ctx, r.Token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("old token must die after the grace period: %v", err)
	}
	if _, err := e.svc.Authenticate(ctx, a.NewToken); err != nil {
		t.Fatalf("new token must work: %v", err)
	}

	// absolute lifetime (12 h) even with constant activity
	tok, start := a.NewToken, e.clk.Now()
	for e.clk.Now().Sub(start) < 13*time.Hour {
		e.clk.Advance(10 * time.Minute)
		b, err := e.svc.Authenticate(ctx, tok)
		if err != nil {
			if e.clk.Now().Sub(r.Session.CreatedAt) < 12*time.Hour {
				t.Fatalf("session died early at %v: %v", e.clk.Now().Sub(r.Session.CreatedAt), err)
			}
			break
		}
		if b.NewToken != "" {
			tok = b.NewToken
		}
	}
	if _, err := e.svc.Authenticate(ctx, tok); !errors.Is(err, ErrNoSession) {
		t.Fatal("the absolute session lifetime (12 h) must end an active session")
	}
	// garbage and revoked tokens
	if _, err := e.svc.Authenticate(ctx, "short"); !errors.Is(err, ErrNoSession) {
		t.Fatal("garbage must be refused")
	}
}

func TestRemoteSessionsHaveShorterIdle(t *testing.T) {
	e := newEnv(t, "none", idents())
	e.enrol(t, "bob", pw)
	// force a remote session through the store (login from remote needs 2FA, which arrives in 1c)
	tok, _, err := e.svc.Sessions.Create(context.Background(), CreateParams{Username: "bob", UID: 1001, Role: identity.User, IP: "203.0.113.1", Origin: netx.Remote, CredEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	e.clk.Advance(16 * time.Minute)
	if _, err := e.svc.Authenticate(context.Background(), tok); !errors.Is(err, ErrNoSession) {
		t.Fatalf("remote idle timeout is 15 min, got %v", err)
	}
}

func TestPasswordChangeRevokesOtherSessionsButNotOwn(t *testing.T) {
	e := newEnv(t, "none", idents())
	e.enrol(t, "bob", pw)
	ctx := context.Background()
	r1, _ := e.svc.Login(ctx, "bob", pw, local)
	r2, _ := e.svc.Login(ctx, "bob", pw, local)
	a1, _ := e.svc.Authenticate(ctx, r1.Token)

	if err := e.svc.ChangePassword(ctx, a1, "not-the-current-pw", "a-brand-new-password", local); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong current password: %v", err)
	}
	var we *WeakPasswordError
	if err := e.svc.ChangePassword(ctx, a1, pw, "short", local); !errors.As(err, &we) {
		t.Fatalf("weak new password: %v", err)
	}
	if err := e.svc.ChangePassword(ctx, a1, pw, "a-brand-new-password", local); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(ctx, r2.Token); !errors.Is(err, ErrNoSession) {
		t.Fatal("other sessions must be revoked after a password change")
	}
	e.clk.Advance(2 * time.Minute) // past the epoch re-check interval
	if _, err := e.svc.Authenticate(ctx, r1.Token); err != nil {
		t.Fatalf("the session that changed the password must survive (epoch adopted): %v", err)
	}
	if _, err := e.svc.Login(ctx, "bob", pw, local); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("old password must stop working")
	}
	if _, err := e.svc.Login(ctx, "bob", "a-brand-new-password", local); err != nil {
		t.Fatal(err)
	}
}

func TestResetViaHelperKillsLiveSessionsWithinTheRecheckInterval(t *testing.T) {
	e := newEnv(t, "none", idents())
	e.enrol(t, "bob", pw)
	ctx := context.Background()
	r, _ := e.svc.Login(ctx, "bob", pw, local)
	if _, err := e.svc.Authenticate(ctx, r.Token); err != nil {
		t.Fatal(err)
	}
	if err := e.root.Call(ctx, "auth.setup.create", priv.Meta{}, map[string]any{"username": "bob", "purpose": "reset"}, nil); err != nil {
		t.Fatal(err)
	}
	e.clk.Advance(61 * time.Second)
	if _, err := e.svc.Authenticate(ctx, r.Token); !errors.Is(err, ErrNoSession) {
		t.Fatal("a reset issued on the host must end existing sessions within 60 s")
	}
}

func TestRoleRemovedFromGroupEndsSession(t *testing.T) {
	id := idents()
	e := newEnv(t, "none", id)
	e.enrol(t, "bob", pw)
	r, _ := e.svc.Login(context.Background(), "bob", pw, local)
	id["bob"] = identity.Info{Username: "bob", UID: 1001, Role: ""} // removed from yubeos-user
	if _, err := e.svc.Authenticate(context.Background(), r.Token); !errors.Is(err, ErrNoSession) {
		t.Fatal("losing the Linux group must end the session")
	}
}

func TestSetupOnlyFromLocalOrigin(t *testing.T) {
	e := newEnv(t, "none", idents())
	var tok struct{ Token string }
	_ = e.root.Call(context.Background(), "auth.setup.create", priv.Meta{}, map[string]any{"username": "bob", "purpose": "first-password"}, &tok)
	if err := e.svc.Setup(context.Background(), tok.Token, pw, remote); !errors.Is(err, ErrNotLocal) {
		t.Fatalf("remote setup must be refused: %v", err)
	}
	if err := e.svc.Setup(context.Background(), "totally-bogus-token-value", pw, local); !errors.Is(err, ErrInvalidSetupToken) {
		t.Fatalf("bogus token: %v", err)
	}
	if err := e.svc.Setup(context.Background(), tok.Token, pw, local); err != nil {
		t.Fatalf("valid local setup: %v", err)
	}
}

func TestHelperDownYieldsUnavailableNotACrash(t *testing.T) {
	e := newEnv(t, "none", idents())
	e.svc.priv = priv.NewClient(filepath.Join(t.TempDir(), "none.sock"))
	if _, err := e.svc.Login(context.Background(), "bob", pw, local); !errors.Is(err, ErrPrivUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestSessionListAndRevoke(t *testing.T) {
	e := newEnv(t, "none", idents())
	e.enrol(t, "bob", pw)
	ctx := context.Background()
	r1, _ := e.svc.Login(ctx, "bob", pw, local)
	_, _ = e.svc.Login(ctx, "bob", pw, local)
	list, err := e.svc.Sessions.List(ctx, "bob")
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v %d", err, len(list))
	}
	for _, s := range list {
		if strings.Contains(s.SID, r1.Token) {
			t.Fatal("cookie value must never appear in listings")
		}
	}
	if ok, _ := e.svc.Sessions.RevokeSID(ctx, "alice", list[0].SID); ok {
		t.Fatal("a user must not revoke someone else's session by sid")
	}
	if ok, _ := e.svc.Sessions.RevokeSID(ctx, "bob", list[0].SID); !ok {
		t.Fatal("own session revoke failed")
	}
	if n, _ := e.svc.Sessions.RevokeUser(ctx, "bob", ""); n != 1 {
		t.Fatalf("revoke-all removed %d, want 1", n)
	}
}
