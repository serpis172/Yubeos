package credstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/serpis172/Yubeos/internal/clock"
	"github.com/serpis172/Yubeos/internal/priv"
	"github.com/serpis172/Yubeos/internal/privd"
	"github.com/serpis172/Yubeos/internal/totp"
)

var fast = Params{M: 1024, T: 1, P: 1} // test-only cost; production floor is MinParams

func newStore(t *testing.T) (*Store, *clock.Fake, string) {
	t.Helper()
	dir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))
	p := fast
	s, err := Open(context.Background(), dir, Options{Params: &p, Clock: clk, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, clk, dir
}

const goodPW = "correct-horse-battery-staple"

func setup(t *testing.T, s *Store, user, pw string) {
	t.Helper()
	tok, _, err := s.CreateSetupLink(context.Background(), user, "first-password", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeSetupLink(context.Background(), tok, pw); err != nil {
		t.Fatal(err)
	}
}

func TestHashFormatAndVerify(t *testing.T) {
	pepper := []byte("0123456789abcdef0123456789abcdef")
	enc, err := Hash(pepper, goodPW, fast)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "$argon2id$v=19$m=1024,t=1,p=1$") || strings.Contains(enc, goodPW) {
		t.Fatalf("unexpected encoding %q", enc)
	}
	ok, rehash, err := Verify(pepper, goodPW, enc, fast)
	if !ok || rehash || err != nil {
		t.Fatalf("verify good: %v %v %v", ok, rehash, err)
	}
	if ok, _, _ := Verify(pepper, goodPW+"x", enc, fast); ok {
		t.Fatal("wrong password accepted")
	}
	if ok, _, _ := Verify([]byte("another-pepper-another-pepper-xx"), goodPW, enc, fast); ok {
		t.Fatal("a different pepper must not verify (pepper is part of the secret)")
	}
	if _, rehash, _ := Verify(pepper, goodPW, enc, Params{M: 2048, T: 1, P: 1}); !rehash {
		t.Fatal("changed parameters must request a rehash")
	}
	// NFKC: composed and decomposed forms of the same password match
	a, _ := Hash(pepper, "caf\u00e9-caf\u00e9-caf\u00e9", fast)
	if ok, _, _ := Verify(pepper, "cafe\u0301-cafe\u0301-cafe\u0301", a, fast); !ok {
		t.Fatal("NFKC normalisation expected")
	}
}

func TestParseRejectsHostileHashes(t *testing.T) {
	for _, bad := range []string{
		"", "plain", "$argon2id$v=19$m=999999999,t=3,p=2$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=19$m=65536,t=999,p=2$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=19$m=65536,t=3,p=0$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5",
		"$argon2i$v=19$m=1024,t=1,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=18$m=1024,t=1,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5",
	} {
		if _, _, err := Verify(nil, "x", bad, fast); err == nil {
			t.Errorf("hash %q must be rejected before any work is done", bad)
		}
	}
}

func TestCalibrateRespectsFloorsAndFastMachines(t *testing.T) {
	p := Calibrate(time.Nanosecond) // impossible target → must shrink to the floor, never below it
	if p.M < MinParams.M || p.T < MinParams.T {
		t.Fatalf("calibration went below the floor: %+v", p)
	}
}

func TestSetupFlowIsSingleUseAndExpires(t *testing.T) {
	s, clk, _ := newStore(t)
	ctx := context.Background()
	tok, _, err := s.CreateSetupLink(ctx, "Alice", "first-password", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// weak password does not burn the link
	if _, err := s.ConsumeSetupLink(ctx, tok, "short"); err == nil {
		t.Fatal("weak password accepted")
	}
	u, err := s.ConsumeSetupLink(ctx, tok, goodPW)
	if err != nil || u != "alice" {
		t.Fatalf("consume: %q %v", u, err)
	}
	if _, err := s.ConsumeSetupLink(ctx, tok, goodPW+"2"); err == nil {
		t.Fatal("setup link must be single-use")
	}
	if ok, _, _ := s.Verify(ctx, "alice", goodPW); !ok {
		t.Fatal("password set by setup link must verify")
	}
	tok2, _, _ := s.CreateSetupLink(ctx, "bob", "first-password", time.Minute)
	clk.Advance(2 * time.Minute)
	if _, err := s.ConsumeSetupLink(ctx, tok2, goodPW); err == nil {
		t.Fatal("expired link accepted")
	}
}

func TestNewLinkInvalidatesPreviousOne(t *testing.T) {
	s, _, _ := newStore(t)
	ctx := context.Background()
	old, _, _ := s.CreateSetupLink(ctx, "carol", "first-password", time.Hour)
	fresh, _, _ := s.CreateSetupLink(ctx, "carol", "first-password", time.Hour)
	if _, err := s.ConsumeSetupLink(ctx, old, goodPW); err == nil {
		t.Fatal("superseded link must be dead")
	}
	if _, err := s.ConsumeSetupLink(ctx, fresh, goodPW); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyIsUniformForUnknownAndWrong(t *testing.T) {
	s, _, _ := newStore(t)
	setup(t, s, "alice", goodPW)
	ctx := context.Background()
	for _, tc := range []struct{ u, p string }{{"alice", "wrong-password-123"}, {"nobody", goodPW}, {"../etc", goodPW}, {"", ""}, {"alice", strings.Repeat("x", 100000)}} {
		ok, epoch, err := s.Verify(ctx, tc.u, tc.p)
		if ok || epoch != 0 || err != nil {
			t.Errorf("Verify(%q) = %v %d %v, want a plain failure", tc.u, ok, epoch, err)
		}
	}
}

func TestEpochBumpsOnChangeAndResetKillsOldPassword(t *testing.T) {
	s, _, _ := newStore(t)
	ctx := context.Background()
	setup(t, s, "dave", goodPW)
	_, e1, _ := s.Verify(ctx, "dave", goodPW)
	if err := s.Change(ctx, "dave", goodPW, "another-long-password-1"); err != nil {
		t.Fatal(err)
	}
	_, e2, _ := s.Verify(ctx, "dave", "another-long-password-1")
	if e2 <= e1 {
		t.Fatalf("epoch must increase on change (%d → %d)", e1, e2)
	}
	if err := s.Change(ctx, "dave", "wrong-current-pw-1", "yet-another-password-2"); err == nil {
		t.Fatal("change with a wrong current password must fail")
	}
	// reset: link creation alone disables the old password
	tok, _, _ := s.CreateSetupLink(ctx, "dave", "reset", time.Hour)
	if ok, _, _ := s.Verify(ctx, "dave", "another-long-password-1"); ok {
		t.Fatal("old password must stop working as soon as a reset link exists")
	}
	if ep, _ := s.Epoch(ctx, "dave"); ep <= e2 {
		t.Fatal("reset must bump the epoch so sessions die")
	}
	if _, err := s.ConsumeSetupLink(ctx, tok, "brand-new-password-3"); err != nil {
		t.Fatal(err)
	}
	if ok, _, _ := s.Verify(ctx, "dave", "brand-new-password-3"); !ok {
		t.Fatal("new password must work after the reset link is consumed")
	}
}

func TestRehashOnParameterUpgrade(t *testing.T) {
	s, _, dir := newStore(t)
	ctx := context.Background()
	setup(t, s, "erin", goodPW)
	s.Close()
	up := Params{M: 2048, T: 1, P: 1}
	s2, err := Open(ctx, dir, Options{Params: &up, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if ok, _, _ := s2.Verify(ctx, "erin", goodPW); !ok {
		t.Fatal("login with old parameters must still work")
	}
	var enc string
	_ = s2.db.QueryRow(`SELECT password_hash FROM credentials WHERE username='erin'`).Scan(&enc)
	if !strings.Contains(enc, "m=2048,") {
		t.Fatalf("hash was not upgraded: %s", enc)
	}
}

func TestFilesAreSecretAndPepperPermissionsChecked(t *testing.T) {
	s, _, dir := newStore(t)
	_ = s
	for _, f := range []string{"credentials.db", "pepper.key"} {
		st, err := os.Stat(filepath.Join(dir, f))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v mode=%o, want 600", f, err, st.Mode().Perm())
		}
	}
	if err := os.Chmod(filepath.Join(dir, "pepper.key"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := fast
	if _, err := Open(context.Background(), dir, Options{Params: &p}); err == nil {
		t.Fatal("a world-readable pepper must be refused")
	}
}

func TestPasswordPolicy(t *testing.T) {
	good := []string{goodPW, "Tr0ub4dor&3-extra", "ein sehr langes passwort!", "пароль-пароль-пароль"}
	for _, p := range good {
		if e := CheckPassword("alice", p); e != nil {
			t.Errorf("%q rejected: %v", p, e)
		}
	}
	bad := map[string]string{
		"short":            "Sh0rt!",
		"username":         "alice",
		"based on user":    "alice-alice1",
		"common":           "password12345",
		"repeated":         "aaaaaaaaaaaaaaaa",
		"sequence":         "abcdefghijklmnop",
		"nul":              "valid-password\x00",
		"too long":         strings.Repeat("a1", 600),
		"common uppercase": "PASSWORD12345",
	}
	for name, p := range bad {
		if e := CheckPassword("alice", p); e == nil || e.Code != priv.CodeWeakPassword {
			t.Errorf("%s: %q must be rejected as weak, got %v", name, p, e)
		}
	}
}

func TestSetupTokenBruteForceIsThrottled(t *testing.T) {
	s, _, _ := newStore(t)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_, _ = s.ConsumeSetupLink(ctx, strings.Repeat("A", 43), goodPW)
	}
	tok, _, _ := s.CreateSetupLink(ctx, "frank", "first-password", time.Hour)
	_, err := s.ConsumeSetupLink(ctx, tok, goodPW)
	var pe *priv.Error
	if !errors.As(err, &pe) || !strings.Contains(pe.Message, "too many") {
		t.Fatalf("expected throttling after repeated bad tokens, got %v", err)
	}
}

func TestRPCMethodsAndRootOnlyEnforcement(t *testing.T) {
	s, _, _ := newStore(t)
	srv := privd.NewServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	Register(srv, s)
	ctx := context.Background()
	asRoot := privd.Direct{Server: srv, UID: 0}
	asDaemon := privd.Direct{Server: srv, UID: 998}

	var tok struct{ Token string }
	if err := asDaemon.Call(ctx, "auth.setup.create", priv.Meta{}, map[string]any{"username": "gina", "purpose": "first-password"}, &tok); err == nil {
		t.Fatal("the daemon (non-root) must NOT be able to create setup links")
	}
	if err := asRoot.Call(ctx, "auth.setup.create", priv.Meta{}, map[string]any{"username": "gina", "purpose": "first-password"}, &tok); err != nil || tok.Token == "" {
		t.Fatalf("root create: %v", err)
	}
	var who struct{ Username string }
	if err := asDaemon.Call(ctx, "auth.setup.consume", priv.Meta{}, map[string]any{"token": tok.Token, "newPassword": goodPW}, &who); err != nil || who.Username != "gina" {
		t.Fatalf("consume: %v %v", err, who)
	}
	var v struct {
		OK    bool
		Epoch int64
	}
	if err := asDaemon.Call(ctx, "auth.password.verify", priv.Meta{}, map[string]any{"username": "gina", "password": goodPW}, &v); err != nil || !v.OK || v.Epoch < 1 {
		t.Fatalf("verify: %v %+v", err, v)
	}
	// unknown fields are rejected by the strict decoder
	if err := asDaemon.Call(ctx, "auth.password.verify", priv.Meta{}, map[string]any{"username": "gina", "password": goodPW, "admin": true}, &v); err == nil {
		t.Fatal("unknown params must be rejected")
	}
}

func TestOpenTightensPreExistingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := fast
	s, err := Open(context.Background(), dir, Options{Params: &p, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Fatalf("state dir mode %o, want 700", st.Mode().Perm())
	}
}

func TestTOTPSecretIsSealedAtRest(t *testing.T) {
	s, _, _ := newStore(t)
	ctx := context.Background()
	secB32, err := s.TOTPBegin(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := totp.DecodeSecret(secB32)
	var blob []byte
	var enc int
	if err := s.db.QueryRowContext(ctx, `SELECT secret, enc FROM totp WHERE username='alice'`).Scan(&blob, &enc); err != nil {
		t.Fatal(err)
	}
	if enc != 1 || bytes.Contains(blob, raw) {
		t.Fatalf("secret must not be stored in clear (enc=%d)", enc)
	}
	ok, _, err := s.TOTPConfirm(ctx, "alice", totp.Code(raw, totp.Step(s.clk.Now())))
	if err != nil || !ok {
		t.Fatalf("sealed secret must still verify: ok=%v err=%v", ok, err)
	}
	// a sealed blob copied to another account does not open (bound to the username)
	if _, err := s.open(blob, "mallory"); err == nil {
		t.Fatal("sealed blob must be bound to its account")
	}
}

func TestLegacyPlainTOTPRowIsMigratedOnFirstUse(t *testing.T) {
	s, _, _ := newStore(t)
	ctx := context.Background()
	raw := totp.NewSecret()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO totp(username, secret, confirmed, last_step, created_at, enc) VALUES('bob',?,1,0,0,0)`, raw); err != nil {
		t.Fatal(err)
	}
	ok, err := s.TOTPVerify(ctx, "bob", totp.Code(raw, totp.Step(s.clk.Now())))
	if err != nil || !ok {
		t.Fatalf("legacy row must verify: ok=%v err=%v", ok, err)
	}
	var enc int
	_ = s.db.QueryRowContext(ctx, `SELECT enc FROM totp WHERE username='bob'`).Scan(&enc)
	if enc != 1 {
		t.Fatal("legacy row must be re-encrypted after first use")
	}
}
