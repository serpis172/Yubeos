package credstore

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/json"
	"errors"
	"strings"

	"github.com/serpis172/Yubeos/internal/priv"
	"github.com/serpis172/Yubeos/internal/privd"
	"github.com/serpis172/Yubeos/internal/totp"
)

const recoveryCodeCount = 10

var recoveryEnc = base32.StdEncoding.WithPadding(base32.NoPadding)

func (s *Store) recoveryHash(code string) []byte {
	code = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
	m := hmac.New(sha256.New, s.pepper)
	m.Write([]byte("recovery:" + code))
	return m.Sum(nil)
}

func newRecoveryCode() string {
	b := make([]byte, 7) // 56 bits → 12 base32 chars (first 10 used)
	if _, err := rand.Read(b); err != nil {
		panic("credstore: crypto/rand failed: " + err.Error())
	}
	c := recoveryEnc.EncodeToString(b)[:10]
	return c[:5] + "-" + c[5:]
}

// TOTPBegin creates (or replaces) a pending TOTP secret. An already confirmed secret must be removed first.
func (s *Store) TOTPBegin(ctx context.Context, username string) (string, error) {
	var confirmed int
	err := s.db.QueryRowContext(ctx, `SELECT confirmed FROM totp WHERE username=?`, username).Scan(&confirmed)
	if err == nil && confirmed == 1 {
		return "", priv.Errorf(priv.CodeInvalidParams, "TOTP is already enrolled; remove it first")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	sec := totp.NewSecret()
	sealed, err := s.seal(sec, username)
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO totp(username, secret, confirmed, last_step, created_at, enc) VALUES(?,?,0,0,?,1)
		ON CONFLICT(username) DO UPDATE SET secret=excluded.secret, confirmed=0, last_step=0, created_at=excluded.created_at, enc=1`, username, sealed, s.now())
	return totp.EncodeSecret(sec), err
}

// TOTPVerify checks a code against the confirmed secret (pending=true: against the pending one, used to confirm).
func (s *Store) totpCheck(ctx context.Context, username, code string, pending bool) (bool, error) {
	var (
		sec  []byte
		conf int
		last int64
		enc  int
	)
	err := s.db.QueryRowContext(ctx, `SELECT secret, confirmed, last_step, enc FROM totp WHERE username=?`, username).Scan(&sec, &conf, &last, &enc)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (conf == 1) == pending) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if enc == 1 {
		if sec, err = s.open(sec, username); err != nil {
			return false, err
		}
	} else if sealed, e := s.seal(sec, username); e == nil { // legacy plaintext row: encrypt it now
		_, _ = s.db.ExecContext(ctx, `UPDATE totp SET secret=?, enc=1 WHERE username=? AND enc=0`, sealed, username)
	}
	step, ok := totp.Verify(sec, code, s.clk.Now(), last)
	if !ok {
		return false, nil
	}
	res, err := s.db.ExecContext(ctx, `UPDATE totp SET last_step=? WHERE username=? AND last_step<?`, step, username, step)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil // a concurrent use of the same step loses
}

// TOTPConfirm activates a pending secret after a valid code and issues recovery codes when none exist.
func (s *Store) TOTPConfirm(ctx context.Context, username, code string) (ok bool, codes []string, err error) {
	if ok, err = s.totpCheck(ctx, username, code, true); err != nil || !ok {
		return false, nil, err
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE totp SET confirmed=1 WHERE username=?`, username); err != nil {
		return false, nil, err
	}
	var left int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM recovery_codes WHERE username=? AND used_at IS NULL`, username).Scan(&left)
	if left == 0 {
		codes, err = s.RecoveryRegenerate(ctx, username)
	}
	return true, codes, err
}

func (s *Store) TOTPVerify(ctx context.Context, username, code string) (bool, error) {
	return s.totpCheck(ctx, username, code, false)
}

// TOTPRemove deletes the secret.
func (s *Store) TOTPRemove(ctx context.Context, username string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM totp WHERE username=?`, username)
	return err
}

// RecoveryRegenerate replaces all recovery codes (old ones stop working) and returns the new plaintext codes once.
func (s *Store) RecoveryRegenerate(ctx context.Context, username string) ([]string, error) {
	codes := make([]string, recoveryCodeCount)
	for i := range codes {
		codes[i] = newRecoveryCode()
	}
	err := s.db.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE username=?`, username); err != nil {
			return err
		}
		for _, c := range codes {
			if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes(username, code_hash) VALUES(?,?)`, username, s.recoveryHash(c)); err != nil {
				return err
			}
		}
		return nil
	})
	return codes, err
}

// RecoveryConsume burns one recovery code; left is the number still unused.
func (s *Store) RecoveryConsume(ctx context.Context, username, code string) (ok bool, left int, err error) {
	res, err := s.db.ExecContext(ctx, `UPDATE recovery_codes SET used_at=? WHERE username=? AND code_hash=? AND used_at IS NULL`,
		s.now(), username, s.recoveryHash(code))
	if err != nil {
		return false, 0, err
	}
	n, _ := res.RowsAffected()
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM recovery_codes WHERE username=? AND used_at IS NULL`, username).Scan(&left)
	return n == 1, left, nil
}

// Factors reports which helper-held factors a user has.
func (s *Store) Factors(ctx context.Context, username string) (hasTOTP bool, recoveryLeft int, err error) {
	var conf int
	if err = s.db.QueryRowContext(ctx, `SELECT confirmed FROM totp WHERE username=?`, username).Scan(&conf); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, 0, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM recovery_codes WHERE username=? AND used_at IS NULL`, username).Scan(&recoveryLeft)
	return conf == 1, recoveryLeft, err
}

type userParam struct {
	Username string `json:"username"`
}

type userCode struct {
	Username string `json:"username"`
	Code     string `json:"code"`
}

// RegisterFactors adds the second-factor RPC methods (TOTP and recovery codes). Passkeys are public-key material
// and live in the daemon database.
func RegisterFactors(srv *privd.Server, st *Store) {
	reg := func(name string, class privd.Class, h privd.Handler) {
		srv.Register(privd.Method{Name: name, Class: class, Handle: h})
	}
	reg("auth.totp.begin", privd.Mutate, func(ctx context.Context, _ priv.Meta, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[userParam](raw)
		if err != nil {
			return nil, err
		}
		sec, err := st.TOTPBegin(ctx, p.Username)
		return struct {
			Secret string `json:"secret"`
		}{sec}, err
	})
	reg("auth.totp.confirm", privd.Mutate, func(ctx context.Context, _ priv.Meta, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[userCode](raw)
		if err != nil {
			return nil, err
		}
		ok, codes, err := st.TOTPConfirm(ctx, p.Username, p.Code)
		return struct {
			OK    bool     `json:"ok"`
			Codes []string `json:"codes,omitempty"`
		}{ok, codes}, err
	})
	reg("auth.totp.verify", privd.Read, func(ctx context.Context, _ priv.Meta, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[userCode](raw)
		if err != nil {
			return nil, err
		}
		ok, err := st.TOTPVerify(ctx, p.Username, p.Code)
		return struct {
			OK bool `json:"ok"`
		}{ok}, err
	})
	reg("auth.totp.remove", privd.Mutate, func(ctx context.Context, _ priv.Meta, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[userParam](raw)
		if err != nil {
			return nil, err
		}
		return struct{}{}, st.TOTPRemove(ctx, p.Username)
	})
	reg("auth.recovery.verify", privd.Mutate, func(ctx context.Context, _ priv.Meta, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[userCode](raw)
		if err != nil {
			return nil, err
		}
		ok, left, err := st.RecoveryConsume(ctx, p.Username, p.Code)
		return struct {
			OK   bool `json:"ok"`
			Left int  `json:"left"`
		}{ok, left}, err
	})
	reg("auth.recovery.regenerate", privd.Mutate, func(ctx context.Context, _ priv.Meta, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[userParam](raw)
		if err != nil {
			return nil, err
		}
		codes, err := st.RecoveryRegenerate(ctx, p.Username)
		return struct {
			Codes []string `json:"codes"`
		}{codes}, err
	})
	reg("auth.factors.get", privd.Read, func(ctx context.Context, _ priv.Meta, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[userParam](raw)
		if err != nil {
			return nil, err
		}
		t, left, err := st.Factors(ctx, p.Username)
		return struct {
			TOTP         bool `json:"totp"`
			RecoveryLeft int  `json:"recoveryLeft"`
		}{t, left}, err
	})
}
