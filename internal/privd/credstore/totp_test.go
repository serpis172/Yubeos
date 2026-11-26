package credstore

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHOTPMatchesRFC4226Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for i, want := range []string{"755224", "287082", "359152", "969429", "338314"} {
		if got := hotp(secret, uint64(i)); got != want {
			t.Fatalf("hotp(%d) = %s, want %s", i, got, want)
		}
	}
	// RFC 6238 appendix B, T=59 s: step 1, last six digits of 94287082
	if got := hotp(secret, 1); got != "287082" {
		t.Fatalf("totp at 59 s: %s", got)
	}
}

func codeNow(t *testing.T, s *Store, b32secret string, offsetSteps int64) string {
	t.Helper()
	raw, err := b32.DecodeString(b32secret)
	if err != nil {
		t.Fatal(err)
	}
	return hotp(raw, uint64(s.clk.Now().Unix()/totpPeriod+offsetSteps))
}

func TestTOTPEnrolVerifyReplayAndRecovery(t *testing.T) {
	ctx := context.Background()
	s, clk, dir := newStore(t)

	secret, err := s.TOTPBegin(ctx, "alice")
	if err != nil || len(secret) != 32 {
		t.Fatalf("begin: %q %v", secret, err)
	}
	if ok, _ := s.TOTPVerify(ctx, "alice", codeNow(t, s, secret, 0)); ok {
		t.Fatal("a pending (unconfirmed) secret must not verify logins")
	}
	if _, err := s.TOTPConfirm(ctx, "alice", "000000"); err == nil {
		t.Fatal("wrong confirming code accepted")
	}
	codes, err := s.TOTPConfirm(ctx, "alice", codeNow(t, s, secret, 0))
	if err != nil || len(codes) != recoveryN {
		t.Fatalf("confirm: %v %v", codes, err)
	}
	if has, left, _ := s.Factors(ctx, "alice"); !has || left != recoveryN {
		t.Fatalf("factors: %v %d", has, left)
	}
	if _, err := s.TOTPBegin(ctx, "alice"); err == nil {
		t.Fatal("re-enrolling over an active TOTP must be refused")
	}

	// the confirming step is burnt: the same code cannot be used to log in right away
	if ok, _ := s.TOTPVerify(ctx, "alice", codeNow(t, s, secret, 0)); ok {
		t.Fatal("replay of the confirming code accepted")
	}
	clk.Advance(30 * time.Second)
	code := codeNow(t, s, secret, 0)
	if ok, err := s.TOTPVerify(ctx, "alice", code); err != nil || !ok {
		t.Fatalf("fresh code refused: %v %v", ok, err)
	}
	if ok, _ := s.TOTPVerify(ctx, "alice", code); ok {
		t.Fatal("a time step must be accepted only once")
	}
	clk.Advance(30 * time.Second)
	if ok, _ := s.TOTPVerify(ctx, "alice", codeNow(t, s, secret, -1)); ok {
		t.Fatal("an older step than the last accepted one must be refused")
	}
	if ok, _ := s.TOTPVerify(ctx, "alice", codeNow(t, s, secret, 1)); !ok {
		t.Fatal("the +1 step inside the skew window must be accepted")
	}
	if ok, _ := s.TOTPVerify(ctx, "bob", code); ok {
		t.Fatal("a user without TOTP must never verify")
	}

	// recovery codes: single use, format-insensitive, replaced on regenerate
	if ok, left, _ := s.RecoveryVerify(ctx, "alice", strings.ToLower(strings.ReplaceAll(codes[0], "-", " "))); !ok || left != recoveryN-1 {
		t.Fatalf("recovery: %v %d", ok, left)
	}
	if ok, _, _ := s.RecoveryVerify(ctx, "alice", codes[0]); ok {
		t.Fatal("a recovery code worked twice")
	}
	fresh, err := s.RecoveryRegenerate(ctx, "alice")
	if err != nil || len(fresh) != recoveryN {
		t.Fatal(err)
	}
	if ok, _, _ := s.RecoveryVerify(ctx, "alice", codes[1]); ok {
		t.Fatal("old recovery code survived regeneration")
	}
	if ok, _, _ := s.RecoveryVerify(ctx, "alice", fresh[0]); !ok {
		t.Fatal("new recovery code refused")
	}

	// the secret is sealed at rest and never stored in clear
	var enc []byte
	if err := s.db.QueryRowContext(ctx, `SELECT secret_enc FROM totp WHERE username='alice'`).Scan(&enc); err != nil {
		t.Fatal(err)
	}
	raw, _ := b32.DecodeString(secret)
	if bytes.Contains(enc, raw) || len(enc) <= len(raw) {
		t.Fatal("TOTP secret stored unencrypted")
	}
	if _, err := s.open("mallory", enc); err == nil {
		t.Fatal("ciphertext must be bound to the username")
	}
	if st, err := osStat(dir + "/secret.key"); err != nil || st&0o077 != 0 {
		t.Fatalf("secret.key mode: %o %v", st, err)
	}

	if err := s.TOTPRemove(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if has, left, _ := s.Factors(ctx, "alice"); has || left != 0 {
		t.Fatalf("after remove: %v %d", has, left)
	}
	if _, err := s.RecoveryRegenerate(ctx, "alice"); err == nil {
		t.Fatal("recovery codes without TOTP must be refused")
	}
}

func TestSecondFactorAttemptsAreRateLimitedInsideTheHelper(t *testing.T) {
	ctx := context.Background()
	s, clk, _ := newStore(t)
	secret, _ := s.TOTPBegin(ctx, "alice")
	codes, _ := s.TOTPConfirm(ctx, "alice", codeNow(t, s, secret, 0))
	clk.Advance(30 * time.Second)
	for i := 0; i < failLimit; i++ {
		if ok, _ := s.TOTPVerify(ctx, "alice", "000000"); ok {
			t.Fatal("bad code accepted")
		}
	}
	// even the right code and a valid recovery code are refused while the limiter is hot
	if ok, _ := s.TOTPVerify(ctx, "alice", codeNow(t, s, secret, 0)); ok {
		t.Fatal("limiter did not engage for TOTP")
	}
	if ok, _, _ := s.RecoveryVerify(ctx, "alice", codes[0]); ok {
		t.Fatal("limiter did not engage for recovery codes")
	}
	clk.Advance(2 * time.Minute)
	if ok, _ := s.TOTPVerify(ctx, "alice", codeNow(t, s, secret, 0)); !ok {
		t.Fatal("limiter did not release after the window")
	}
}

func osStat(p string) (uint32, error) {
	st, err := os.Stat(p)
	if err != nil {
		return 0, err
	}
	return uint32(st.Mode().Perm()), nil
}
