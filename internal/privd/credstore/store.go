// Package credstore is the helper-owned credential store and the auth.* RPC methods
// (docs/AUTHENTICATION.md §3, ADR-0016). Password verifiers never leave the root helper.
package credstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/serpis172/Yubeos/internal/clock"
	"github.com/serpis172/Yubeos/internal/db"
	"github.com/serpis172/Yubeos/internal/priv"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations returns the credentials.db migrations.
func Migrations() fs.FS {
	sub, _ := fs.Sub(migrationFiles, "migrations")
	return sub
}

// ValidUsername matches Linux login names (lower-case form).
var ValidUsername = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// Options configures Open.
type Options struct {
	Params          *Params       // fixed parameters (tests); default: load persisted or calibrate
	CalibrateTarget time.Duration // default 250 ms
	Clock           clock.Clock
	Log             *slog.Logger
}

// Store holds credentials.db, the pepper and the active Argon2id parameters.
type Store struct {
	db     *db.DB
	clk    clock.Clock
	log    *slog.Logger
	pepper []byte
	params Params
	dummy  string

	mu         sync.Mutex
	consumeErr []time.Time // recent failed setup-token attempts (global limiter)
}

// Open prepares dir (0700): credentials.db (0600), pepper.key (0600, created once), parameters.
func Open(ctx context.Context, dir string, o Options) (*Store, error) {
	if o.Clock == nil {
		o.Clock = clock.Real{}
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.CalibrateTarget == 0 {
		o.CalibrateTarget = 250 * time.Millisecond
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// The directory may pre-exist with looser permissions (installer, manual mkdir): the helper owns it, so tighten it.
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("credstore: cannot restrict %s to 0700: %w", dir, err)
	}
	pepper, err := loadOrCreatePepper(filepath.Join(dir, "pepper.key"))
	if err != nil {
		return nil, err
	}
	d, err := db.Open(ctx, filepath.Join(dir, "credentials.db"), Migrations(), o.Log)
	if err != nil {
		return nil, err
	}
	s := &Store{db: d, clk: o.Clock, log: o.Log, pepper: pepper}
	switch {
	case o.Params != nil:
		s.params = *o.Params
	default:
		var raw string
		err := d.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='argon2'`).Scan(&raw)
		if err == nil && json.Unmarshal([]byte(raw), &s.params) == nil && s.params.M >= MinParams.M {
			break
		}
		s.params = Calibrate(o.CalibrateTarget)
		b, _ := json.Marshal(s.params)
		if _, err := d.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('argon2',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(b)); err != nil {
			return nil, err
		}
		o.Log.Info("argon2id parameters calibrated", "m_kib", s.params.M, "t", s.params.T, "p", s.params.P)
	}
	if s.dummy, err = Hash(pepper, "never-matches-"+randomToken(), s.params); err != nil {
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Params returns the active parameters.
func (s *Store) Params() Params { return s.params }

func loadOrCreatePepper(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil {
		st, _ := os.Stat(path)
		if st.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf("credstore: %s must not be accessible by group/others (mode %o)", path, st.Mode().Perm())
		}
		if len(b) < 32 {
			return nil, fmt.Errorf("credstore: %s is too short", path)
		}
		return b, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return nil, err
	}
	return b, nil
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(tok string) []byte { h := sha256.Sum256([]byte(tok)); return h[:] }

func (s *Store) now() int64 { return s.clk.Now().UnixMilli() }

// Verify checks a password. Unknown users, users pending a reset and wrong passwords are indistinguishable
// (same work, same result). On success with outdated parameters the hash is upgraded in place.
func (s *Store) Verify(ctx context.Context, username, password string) (ok bool, epoch int64, err error) {
	username = strings.ToLower(username)
	var enc string
	var pending int
	found := false
	if ValidUsername.MatchString(username) {
		switch err := s.db.QueryRowContext(ctx, `SELECT password_hash, epoch, pending_reset FROM credentials WHERE username=?`, username).Scan(&enc, &epoch, &pending); {
		case err == nil:
			found = true
		case !errors.Is(err, sql.ErrNoRows):
			return false, 0, err
		}
	}
	if len(password) > MaxPasswordRunes*4 { // never hash absurd inputs
		password = ""
	}
	if !found || pending == 1 {
		_, _, _ = Verify(s.pepper, password, s.dummy, s.params) // equalise timing
		return false, 0, nil
	}
	good, rehash, err := Verify(s.pepper, password, enc, s.params)
	if err != nil {
		s.log.Error("credstore: stored hash unreadable", "user", username)
		return false, 0, nil
	}
	if good && rehash {
		if nh, err := Hash(s.pepper, password, s.params); err == nil {
			_, _ = s.db.ExecContext(ctx, `UPDATE credentials SET password_hash=? WHERE username=? AND password_hash=?`, nh, username, enc)
		}
	}
	if !good {
		return false, 0, nil
	}
	return true, epoch, nil
}

// setPassword stores a new hash and bumps the epoch (all sessions of the user become invalid).
func (s *Store) setPassword(ctx context.Context, q interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, username, password string) error {
	enc, err := Hash(s.pepper, password, s.params)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO credentials(username, password_hash, epoch, pending_reset, updated_at) VALUES(?,?,1,0,?)
		ON CONFLICT(username) DO UPDATE SET password_hash=excluded.password_hash, epoch=credentials.epoch+1, pending_reset=0, updated_at=excluded.updated_at`,
		username, enc, s.now())
	return err
}

// Change replaces the password after verifying the current one.
func (s *Store) Change(ctx context.Context, username, current, next string) error {
	username = strings.ToLower(username)
	ok, _, err := s.Verify(ctx, username, current)
	if err != nil {
		return err
	}
	if !ok {
		return priv.Errorf(priv.CodeForbidden, "current password is incorrect")
	}
	if pe := CheckPassword(username, next); pe != nil {
		return pe
	}
	return s.setPassword(ctx, s.db, username, next)
}

// CreateSetupLink issues a single-use token (returned once) valid for ttl. purpose "reset" additionally disables the
// current password immediately and bumps the epoch, so old sessions and the old password die at once.
func (s *Store) CreateSetupLink(ctx context.Context, username, purpose string, ttl time.Duration) (token string, expires time.Time, err error) {
	username = strings.ToLower(username)
	if !ValidUsername.MatchString(username) {
		return "", time.Time{}, priv.Errorf(priv.CodeInvalidParams, "invalid username")
	}
	if purpose != "first-password" && purpose != "reset" {
		return "", time.Time{}, priv.Errorf(priv.CodeInvalidParams, "invalid purpose")
	}
	token = randomToken()
	expires = s.clk.Now().Add(ttl)
	err = s.db.InTx(ctx, func(tx *sql.Tx) error {
		// at most one live link per user
		if _, err := tx.ExecContext(ctx, `UPDATE setup_links SET used_at=? WHERE username=? AND used_at IS NULL`, s.now(), username); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO setup_links(token_hash, username, purpose, created_at, expires_at) VALUES(?,?,?,?,?)`,
			hashToken(token), username, purpose, s.now(), expires.UnixMilli()); err != nil {
			return err
		}
		if purpose == "reset" {
			_, err := tx.ExecContext(ctx, `UPDATE credentials SET pending_reset=1, epoch=epoch+1 WHERE username=?`, username)
			return err
		}
		return nil
	})
	return token, expires, err
}

func (s *Store) throttled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cut := s.clk.Now().Add(-time.Minute)
	kept := s.consumeErr[:0]
	for _, t := range s.consumeErr {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	s.consumeErr = kept
	return len(kept) >= 10
}

func (s *Store) noteBadToken() {
	s.mu.Lock()
	s.consumeErr = append(s.consumeErr, s.clk.Now())
	s.mu.Unlock()
}

// ConsumeSetupLink validates the token and sets the password. A weak password does NOT consume the token.
func (s *Store) ConsumeSetupLink(ctx context.Context, token, password string) (username string, err error) {
	if s.throttled() {
		return "", priv.Errorf(priv.CodeInvalidToken, "too many attempts, try again later")
	}
	bad := func() (string, error) {
		s.noteBadToken()
		return "", priv.Errorf(priv.CodeInvalidToken, "setup link is invalid or expired")
	}
	if len(token) < 20 || len(token) > 200 {
		return bad()
	}
	var expires int64
	var used sql.NullInt64
	err = s.db.QueryRowContext(ctx, `SELECT username, expires_at, used_at FROM setup_links WHERE token_hash=?`, hashToken(token)).Scan(&username, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (used.Valid || expires < s.now())) {
		return bad()
	}
	if err != nil {
		return "", err
	}
	if pe := CheckPassword(username, password); pe != nil {
		return "", pe
	}
	err = s.db.InTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE setup_links SET used_at=? WHERE token_hash=? AND used_at IS NULL`, s.now(), hashToken(token))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return priv.Errorf(priv.CodeInvalidToken, "setup link is invalid or expired")
		}
		return s.setPassword(ctx, tx, username, password)
	})
	return username, err
}

// Epoch returns the credential epoch of a user (0 if unknown).
func (s *Store) Epoch(ctx context.Context, username string) (int64, error) {
	var e int64
	err := s.db.QueryRowContext(ctx, `SELECT epoch FROM credentials WHERE username=?`, strings.ToLower(username)).Scan(&e)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return e, err
}
