package credstore

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 default HMAC-SHA1 for authenticator-app compatibility
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/serpis172/Yubeos/internal/priv"
)

// RFC 6238, SHA-1, 6 digits, 30 s: the combination every authenticator app supports.
const (
	totpPeriod   = 30
	totpSkew     = 1 // ±1 step
	recoveryN    = 10
	failLimit    = 10 // failed second-factor checks per user per minute, enforced inside the helper too
	failWindow   = time.Minute
	totpSecretLn = 20 // 160 bit
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func hotp(secret []byte, counter uint64) string {
	var c [8]byte
	binary.BigEndian.PutUint64(c[:], counter)
	m := hmac.New(sha1.New, secret) //nolint:gosec
	m.Write(c[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0F
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7FFFFFFF
	return fmt.Sprintf("%06d", v%1_000_000)
}

// matchStep returns the accepted time step for code, or 0. Steps at or below minStep are refused (replay).
func matchStep(secret []byte, code string, now time.Time, minStep int64) int64 {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0
	}
	cur := now.Unix() / totpPeriod
	var hit int64
	for d := int64(-totpSkew); d <= totpSkew; d++ { // always evaluate every candidate: constant work
		step := cur + d
		if subtle.ConstantTimeCompare([]byte(code), []byte(hotp(secret, uint64(step)))) == 1 && step > minStep && step > hit {
			hit = step
		}
	}
	return hit
}

func (s *Store) seal(user string, secret []byte) ([]byte, error) {
	blk, err := aes.NewCipher(s.secretKey)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	n := make([]byte, g.NonceSize())
	if _, err := rand.Read(n); err != nil {
		return nil, err
	}
	return g.Seal(n, n, secret, []byte(user)), nil
}

func (s *Store) open(user string, enc []byte) ([]byte, error) {
	blk, err := aes.NewCipher(s.secretKey)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	if len(enc) < g.NonceSize() {
		return nil, errors.New("credstore: truncated totp secret")
	}
	return g.Open(nil, enc[:g.NonceSize()], enc[g.NonceSize():], []byte(user))
}

func (s *Store) limited(user string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cut := s.clk.Now().Add(-failWindow)
	kept := s.factorFails[user][:0]
	for _, t := range s.factorFails[user] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	s.factorFails[user] = kept
	return len(kept) >= failLimit
}

func (s *Store) noteFail(user string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.factorFails[user] = append(s.factorFails[user], s.clk.Now())
}

func normCode(c string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(c)))
}

func hashCode(c string) []byte { h := sha256.Sum256([]byte(normCode(c))); return h[:] }

// newRecoveryCodes returns display forms ("ABCD-EFGH-IJKL-MNOP", 80 random bits each).
func newRecoveryCodes() []string {
	out := make([]string, recoveryN)
	for i := range out {
		b := make([]byte, 10)
		_, _ = rand.Read(b)
		c := b32.EncodeToString(b) // 16 characters
		out[i] = c[0:4] + "-" + c[4:8] + "-" + c[8:12] + "-" + c[12:16]
	}
	return out
}

func (s *Store) storeCodes(ctx context.Context, tx *sql.Tx, user string, codes []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE username=?`, user); err != nil {
		return err
	}
	for _, c := range codes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes(username, hash) VALUES(?,?)`, user, hashCode(c)); err != nil {
			return err
		}
	}
	return nil
}

// TOTPBegin generates a pending secret (replacing an unconfirmed one) and returns it base32-encoded.
func (s *Store) TOTPBegin(ctx context.Context, user string) (string, error) {
	var active int
	err := s.db.QueryRowContext(ctx, `SELECT active FROM totp WHERE username=?`, user).Scan(&active)
	if err == nil && active == 1 {
		return "", priv.Errorf(priv.CodeInvalidParams, "TOTP is already enabled; remove it first")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	secret := make([]byte, totpSecretLn)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	enc, err := s.seal(user, secret)
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO totp(username, secret_enc, active, last_step, created_at) VALUES(?,?,0,0,?)
		ON CONFLICT(username) DO UPDATE SET secret_enc=excluded.secret_enc, active=0, last_step=0, created_at=excluded.created_at`,
		user, enc, s.now())
	return b32.EncodeToString(secret), err
}

func (s *Store) loadTOTP(ctx context.Context, user string, wantActive bool) (secret []byte, lastStep int64, err error) {
	var enc []byte
	var active int
	err = s.db.QueryRowContext(ctx, `SELECT secret_enc, active, last_step FROM totp WHERE username=?`, user).Scan(&enc, &active, &lastStep)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (active == 1) != wantActive {
		return nil, 0, sql.ErrNoRows
	}
	if err != nil {
		return nil, 0, err
	}
	secret, err = s.open(user, enc)
	return secret, lastStep, err
}

// TOTPConfirm activates the pending secret when code matches and returns the one-time recovery codes.
func (s *Store) TOTPConfirm(ctx context.Context, user, code string) ([]string, error) {
	if s.limited(user) {
		return nil, priv.Errorf(priv.CodeForbidden, "too many attempts")
	}
	secret, _, err := s.loadTOTP(ctx, user, false)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, priv.Errorf(priv.CodeNotFound, "no TOTP enrolment in progress")
	}
	if err != nil {
		return nil, err
	}
	step := matchStep(secret, code, s.clk.Now(), 0)
	if step == 0 {
		s.noteFail(user)
		return nil, priv.Errorf(priv.CodeForbidden, "invalid code")
	}
	codes := newRecoveryCodes()
	err = s.db.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE totp SET active=1, last_step=? WHERE username=?`, step, user); err != nil {
			return err
		}
		return s.storeCodes(ctx, tx, user, codes)
	})
	return codes, err
}

// TOTPVerify checks a code against the active secret. A time step is accepted once.
func (s *Store) TOTPVerify(ctx context.Context, user, code string) (bool, error) {
	if s.limited(user) {
		return false, nil
	}
	secret, last, err := s.loadTOTP(ctx, user, true)
	if errors.Is(err, sql.ErrNoRows) {
		s.noteFail(user)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	step := matchStep(secret, code, s.clk.Now(), last)
	if step == 0 {
		s.noteFail(user)
		return false, nil
	}
	res, err := s.db.ExecContext(ctx, `UPDATE totp SET last_step=? WHERE username=? AND last_step<?`, step, user, step)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n != 1 { // lost a race with a parallel use of the same code
		s.noteFail(user)
		return false, nil
	}
	return true, nil
}

// RecoveryVerify consumes one recovery code.
func (s *Store) RecoveryVerify(ctx context.Context, user, code string) (ok bool, left int, err error) {
	if s.limited(user) {
		return false, 0, nil
	}
	res, err := s.db.ExecContext(ctx, `UPDATE recovery_codes SET used_at=? WHERE username=? AND hash=? AND used_at IS NULL`, s.now(), user, hashCode(code))
	if err != nil {
		return false, 0, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		s.noteFail(user)
		return false, 0, nil
	}
	_, left, err = s.Factors(ctx, user)
	return true, left, err
}

// RecoveryRegenerate replaces all recovery codes (the old ones stop working). Needs an active TOTP.
func (s *Store) RecoveryRegenerate(ctx context.Context, user string) ([]string, error) {
	if has, _, err := s.Factors(ctx, user); err != nil {
		return nil, err
	} else if !has {
		return nil, priv.Errorf(priv.CodeInvalidParams, "enable TOTP first")
	}
	codes := newRecoveryCodes()
	return codes, s.db.InTx(ctx, func(tx *sql.Tx) error { return s.storeCodes(ctx, tx, user, codes) })
}

// TOTPRemove deletes the TOTP secret and the recovery codes.
func (s *Store) TOTPRemove(ctx context.Context, user string) error {
	return s.db.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM totp WHERE username=?`, user); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE username=?`, user)
		return err
	})
}

// Factors reports whether TOTP is active and how many recovery codes are left.
func (s *Store) Factors(ctx context.Context, user string) (totp bool, recoveryLeft int, err error) {
	var active int
	switch err = s.db.QueryRowContext(ctx, `SELECT active FROM totp WHERE username=?`, user).Scan(&active); {
	case errors.Is(err, sql.ErrNoRows):
		return false, 0, nil
	case err != nil:
		return false, 0, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM recovery_codes WHERE username=? AND used_at IS NULL`, user).Scan(&recoveryLeft)
	return active == 1, recoveryLeft, err
}
