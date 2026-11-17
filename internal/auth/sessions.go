package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/serpis172/Yubeos/internal/clock"
	"github.com/serpis172/Yubeos/internal/db"
	"github.com/serpis172/Yubeos/internal/identity"
	"github.com/serpis172/Yubeos/internal/netx"
)

// ErrNoSession means the cookie is unknown, expired or revoked.
var ErrNoSession = errors.New("auth: no valid session")

// SessionConfig holds the lifetimes (docs/AUTHENTICATION.md §6).
type SessionConfig struct {
	IdleLocal, IdleRemote time.Duration
	MaxAge                time.Duration
	RotateEvery           time.Duration // new cookie value at most this often (default 15 min)
	PrevGrace             time.Duration // the previous value keeps working briefly (parallel requests), default 30 s
}

func (c SessionConfig) withDefaults() SessionConfig {
	if c.RotateEvery == 0 {
		c.RotateEvery = 15 * time.Minute
	}
	if c.PrevGrace == 0 {
		c.PrevGrace = 30 * time.Second
	}
	return c
}

// Session is the server-side login state.
type Session struct {
	SID                string
	Username           string
	CSRF               string
	IP, UserAgent      string
	Origin             netx.Origin
	Factors            []string
	EnrollmentRequired bool
	CredEpoch          int64
	CreatedAt          time.Time
	LastSeenAt         time.Time
	ExpiresAt          time.Time
}

// Sessions stores sessions in SQLite; only SHA-256 of the cookie value is persisted.
type Sessions struct {
	db  *db.DB
	clk clock.Clock
	cfg SessionConfig
}

func NewSessions(d *db.DB, clk clock.Clock, cfg SessionConfig) *Sessions {
	if clk == nil {
		clk = clock.Real{}
	}
	return &Sessions{db: d, clk: clk, cfg: cfg.withDefaults()}
}

func newSecret(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("auth: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashTok(t string) []byte { h := sha256.Sum256([]byte(t)); return h[:] }

func newSID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// CreateParams describes a new session.
type CreateParams struct {
	Username           string
	UID                uint32
	Role               identity.Role
	IP, UserAgent      string
	Origin             netx.Origin
	Factors            []string
	EnrollmentRequired bool
	CredEpoch          int64
}

func (s *Sessions) idle(o netx.Origin) time.Duration {
	if o == netx.Remote && s.cfg.IdleRemote > 0 {
		return s.cfg.IdleRemote
	}
	return s.cfg.IdleLocal
}

// Create stores a session (and upserts the users_meta row) and returns the cookie value.
func (s *Sessions) Create(ctx context.Context, p CreateParams) (string, Session, error) {
	now := s.clk.Now()
	tok := newSecret(32)
	sess := Session{SID: newSID(), Username: p.Username, CSRF: newSecret(32), IP: p.IP, UserAgent: truncate(p.UserAgent, 300),
		Origin: p.Origin, Factors: p.Factors, EnrollmentRequired: p.EnrollmentRequired, CredEpoch: p.CredEpoch,
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(s.cfg.MaxAge)}
	factors, _ := json.Marshal(p.Factors)
	err := s.db.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO users_meta(username, uid, role, enabled, last_login_at, created_at) VALUES(?,?,?,1,?,?)
			ON CONFLICT(username) DO UPDATE SET uid=excluded.uid, role=excluded.role, last_login_at=excluded.last_login_at`,
			p.Username, p.UID, string(p.Role), now.UnixMilli(), now.UnixMilli()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO sessions(id_hash, sid, username, csrf, ip, user_agent, origin, factors, enrollment_required, cred_epoch,
			created_at, rotated_at, last_seen_at, expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			hashTok(tok), sess.SID, p.Username, sess.CSRF, p.IP, sess.UserAgent, string(p.Origin), string(factors), b2i(p.EnrollmentRequired), p.CredEpoch,
			now.UnixMilli(), now.UnixMilli(), now.UnixMilli(), sess.ExpiresAt.UnixMilli())
		return err
	})
	return tok, sess, err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

const touchEvery = 30 * time.Second

// Lookup validates a cookie value. newToken is non-empty when the cookie value was rotated and must be re-sent.
func (s *Sessions) Lookup(ctx context.Context, token string) (sess *Session, newToken string, err error) {
	if len(token) < 32 || len(token) > 128 {
		return nil, "", ErrNoSession
	}
	now := s.clk.Now()
	h := hashTok(token)
	var (
		out          Session
		origin       string
		factors      string
		enr          int
		created, rot int64
		seen, exp    int64
		viaPrev      bool
		ip, ua       sql.NullString
		curHash      []byte
		prevUntil    sql.NullInt64
	)
	row := s.db.QueryRowContext(ctx, `SELECT id_hash, sid, username, csrf, ip, user_agent, origin, factors, enrollment_required, cred_epoch,
		created_at, rotated_at, last_seen_at, expires_at, prev_valid_until FROM sessions WHERE id_hash=? OR (prev_id_hash=? AND prev_valid_until>?)`,
		h, h, now.UnixMilli())
	if err := row.Scan(&curHash, &out.SID, &out.Username, &out.CSRF, &ip, &ua, &origin, &factors, &enr, &out.CredEpoch, &created, &rot, &seen, &exp, &prevUntil); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrNoSession
		}
		return nil, "", err
	}
	viaPrev = string(curHash) != string(h)
	out.IP, out.UserAgent = ip.String, ua.String
	out.Origin = netx.Origin(origin)
	out.EnrollmentRequired = enr == 1
	_ = json.Unmarshal([]byte(factors), &out.Factors)
	out.CreatedAt, out.LastSeenAt, out.ExpiresAt = time.UnixMilli(created), time.UnixMilli(seen), time.UnixMilli(exp)

	if !now.Before(out.ExpiresAt) || now.Sub(out.LastSeenAt) > s.idle(out.Origin) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE sid=?`, out.SID)
		return nil, "", ErrNoSession
	}
	if !viaPrev && now.Sub(time.UnixMilli(rot)) >= s.cfg.RotateEvery {
		newToken = newSecret(32)
		_, err = s.db.ExecContext(ctx, `UPDATE sessions SET id_hash=?, prev_id_hash=?, prev_valid_until=?, rotated_at=?, last_seen_at=? WHERE sid=?`,
			hashTok(newToken), h, now.Add(s.cfg.PrevGrace).UnixMilli(), now.UnixMilli(), now.UnixMilli(), out.SID)
		if err != nil {
			return nil, "", err
		}
		out.LastSeenAt = now
	} else if now.Sub(out.LastSeenAt) >= touchEvery {
		if _, err = s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at=? WHERE sid=?`, now.UnixMilli(), out.SID); err != nil {
			return nil, "", err
		}
		out.LastSeenAt = now
	}
	return &out, newToken, nil
}

// SetEpoch records the credential epoch a session is valid for (used after the user's own password change).
func (s *Sessions) SetEpoch(ctx context.Context, sid string, epoch int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET cred_epoch=? WHERE sid=?`, epoch, sid)
	return err
}

// Revoke deletes the session identified by its cookie value.
func (s *Sessions) Revoke(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash=?`, hashTok(token))
	return err
}

// RevokeSID deletes one of username's sessions by public id; it reports whether something was deleted.
func (s *Sessions) RevokeSID(ctx context.Context, username, sid string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE username=? AND sid=?`, username, sid)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RevokeUser deletes all of username's sessions except exceptSID ("" = all).
func (s *Sessions) RevokeUser(ctx context.Context, username, exceptSID string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE username=? AND sid<>?`, username, exceptSID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SessionInfo is the listing view of a session (never contains secrets).
type SessionInfo struct {
	SID        string
	IP         string
	UserAgent  string
	Origin     netx.Origin
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

// List returns the live sessions of a user, newest first.
func (s *Sessions) List(ctx context.Context, username string) ([]SessionInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sid, ip, user_agent, origin, created_at, last_seen_at, expires_at FROM sessions
		WHERE username=? AND expires_at>? ORDER BY last_seen_at DESC`, username, s.clk.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionInfo
	for rows.Next() {
		var si SessionInfo
		var ip, ua sql.NullString
		var origin string
		var c, l, e int64
		if err := rows.Scan(&si.SID, &ip, &ua, &origin, &c, &l, &e); err != nil {
			return nil, err
		}
		si.IP, si.UserAgent, si.Origin = ip.String, ua.String, netx.Origin(origin)
		si.CreatedAt, si.LastSeenAt, si.ExpiresAt = time.UnixMilli(c), time.UnixMilli(l), time.UnixMilli(e)
		out = append(out, si)
	}
	return out, rows.Err()
}

// Purge removes expired sessions and old failure records; call periodically.
func (s *Sessions) Purge(ctx context.Context) error {
	now := s.clk.Now()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at<?`, now.UnixMilli()); err != nil {
		return err
	}
	idle := s.cfg.IdleLocal
	if s.cfg.IdleRemote > idle {
		idle = s.cfg.IdleRemote
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE last_seen_at<?`, now.Add(-idle).UnixMilli())
	return err
}
