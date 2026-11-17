package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/serpis172/Yubeos/internal/clock"
	"github.com/serpis172/Yubeos/internal/db"
	"github.com/serpis172/Yubeos/internal/netx"
)

// ThrottleConfig: docs/AUTHENTICATION.md §8.
type ThrottleConfig struct {
	LockoutThreshold int           // consecutive failures per account before a soft lockout (default 10)
	LockoutDuration  time.Duration // default 15 min
	IPWindow         time.Duration // default 10 min
	IPMaxFailures    int           // failures per IP within IPWindow before a ban (default 20), REMOTE origins only
	IPBanBase        time.Duration // first ban (default 1 h); doubles per repeat
	IPBanMax         time.Duration // cap (default 24 h)
}

func (c ThrottleConfig) withDefaults() ThrottleConfig {
	if c.LockoutThreshold == 0 {
		c.LockoutThreshold = 10
	}
	if c.LockoutDuration == 0 {
		c.LockoutDuration = 15 * time.Minute
	}
	if c.IPWindow == 0 {
		c.IPWindow = 10 * time.Minute
	}
	if c.IPMaxFailures == 0 {
		c.IPMaxFailures = 20
	}
	if c.IPBanBase == 0 {
		c.IPBanBase = time.Hour
	}
	if c.IPBanMax == 0 {
		c.IPBanMax = 24 * time.Hour
	}
	return c
}

// Throttle implements account lockout and IP bans on top of login_failures / ip_bans.
type Throttle struct {
	db  *db.DB
	clk clock.Clock
	cfg ThrottleConfig
}

func NewThrottle(d *db.DB, clk clock.Clock, cfg ThrottleConfig) *Throttle {
	if clk == nil {
		clk = clock.Real{}
	}
	return &Throttle{db: d, clk: clk, cfg: cfg.withDefaults()}
}

const okMarker = "ok"

// normUser bounds and lower-cases the supplied login name. Lockout applies to ANY supplied name, existing or not,
// so lockout behaviour cannot be used to enumerate accounts.
func normUser(u string) string {
	u = strings.ToLower(strings.TrimSpace(u))
	if len(u) > 64 {
		u = u[:64]
	}
	return u
}

// AccountLocked reports a soft lockout of password logins for username.
func (t *Throttle) AccountLocked(ctx context.Context, username string) (until time.Time, locked bool, err error) {
	username = normUser(username)
	now := t.clk.Now()
	var lastOK sql.NullInt64
	if err = t.db.QueryRowContext(ctx, `SELECT MAX(ts) FROM login_failures WHERE username=? AND reason=?`, username, okMarker).Scan(&lastOK); err != nil {
		return
	}
	since := now.Add(-24 * time.Hour).UnixMilli()
	if lastOK.Valid && lastOK.Int64 > since {
		since = lastOK.Int64
	}
	var n int
	var lastFail sql.NullInt64
	if err = t.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(ts) FROM login_failures WHERE username=? AND reason<>? AND ts>?`, username, okMarker, since).Scan(&n, &lastFail); err != nil {
		return
	}
	if n < t.cfg.LockoutThreshold || !lastFail.Valid {
		return time.Time{}, false, nil
	}
	until = time.UnixMilli(lastFail.Int64).Add(t.cfg.LockoutDuration)
	return until, now.Before(until), nil
}

// IPBanned reports an active ban of ip (bans only ever exist for remote origins).
func (t *Throttle) IPBanned(ctx context.Context, ip string) (until time.Time, banned bool, err error) {
	var u int64
	err = t.db.QueryRowContext(ctx, `SELECT until FROM ip_bans WHERE ip=?`, ip).Scan(&u)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return
	}
	until = time.UnixMilli(u)
	return until, t.clk.Now().Before(until), nil
}

// Failure records a failed attempt. lockedNow / bannedNow are true when this failure crossed a threshold.
func (t *Throttle) Failure(ctx context.Context, username, ip string, origin netx.Origin, reason string) (lockedNow, bannedNow bool, err error) {
	username = normUser(username)
	now := t.clk.Now()
	wasLocked := false
	if _, wasLocked, err = t.AccountLocked(ctx, username); err != nil {
		return
	}
	if _, err = t.db.ExecContext(ctx, `INSERT INTO login_failures(ts, username, ip, reason) VALUES(?,?,?,?)`, now.UnixMilli(), username, ip, reason); err != nil {
		return
	}
	if _, nowLocked, e := t.AccountLocked(ctx, username); e == nil {
		lockedNow = nowLocked && !wasLocked
	}
	if origin != netx.Remote {
		return
	}
	var n int
	if err = t.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM login_failures WHERE ip=? AND reason<>? AND ts>?`, ip, okMarker, now.Add(-t.cfg.IPWindow).UnixMilli()).Scan(&n); err != nil {
		return
	}
	if n < t.cfg.IPMaxFailures {
		return
	}
	if _, already, _ := t.IPBanned(ctx, ip); already {
		return
	}
	strikes := 1
	var prev int
	if e := t.db.QueryRowContext(ctx, `SELECT strikes FROM ip_bans WHERE ip=?`, ip).Scan(&prev); e == nil {
		strikes = prev + 1
	}
	d := t.cfg.IPBanBase
	for i := 1; i < strikes && d < t.cfg.IPBanMax; i++ {
		d *= 2
	}
	if d > t.cfg.IPBanMax {
		d = t.cfg.IPBanMax
	}
	_, err = t.db.ExecContext(ctx, `INSERT INTO ip_bans(ip, until, strikes, reason) VALUES(?,?,?,?)
		ON CONFLICT(ip) DO UPDATE SET until=excluded.until, strikes=excluded.strikes, reason=excluded.reason`,
		ip, now.Add(d).UnixMilli(), strikes, "too many failed logins")
	bannedNow = err == nil
	return
}

// Success marks a successful authentication, which resets the account's failure streak.
func (t *Throttle) Success(ctx context.Context, username, ip string) error {
	_, err := t.db.ExecContext(ctx, `INSERT INTO login_failures(ts, username, ip, reason) VALUES(?,?,?,?)`, t.clk.Now().UnixMilli(), normUser(username), ip, okMarker)
	return err
}

// Purge drops failure records older than 30 days and bans that expired more than a day ago.
func (t *Throttle) Purge(ctx context.Context) error {
	now := t.clk.Now()
	if _, err := t.db.ExecContext(ctx, `DELETE FROM login_failures WHERE ts<?`, now.AddDate(0, 0, -30).UnixMilli()); err != nil {
		return err
	}
	_, err := t.db.ExecContext(ctx, `DELETE FROM ip_bans WHERE until<?`, now.Add(-24*time.Hour).UnixMilli())
	return err
}
