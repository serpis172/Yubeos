// Package audit is the append-only, hash-chained audit log (docs/SECURITY.md §7).
package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/serpis172/Yubeos/internal/clock"
	"github.com/serpis172/Yubeos/internal/db"
)

// Result values stored in audit_log.result.
const (
	OK     = "ok"
	Fail   = "fail"
	Denied = "denied"
)

// Entry is one audit event. Params must already be free of secrets (use Redact).
type Entry struct {
	Actor, Role, IP, ReqID string
	Action, Target         string
	Params                 map[string]any
	Result                 string
	JobID                  string
}

// Log writes and verifies the chain.
type Log struct {
	db    *db.DB
	clk   clock.Clock
	log   *slog.Logger
	mu    sync.Mutex
	hooks []func(Entry)
}

func New(d *db.DB, clk clock.Clock, log *slog.Logger) *Log {
	if log == nil {
		log = slog.Default()
	}
	if clk == nil {
		clk = clock.Real{}
	}
	return &Log{db: d, clk: clk, log: log.With("audit", true)}
}

var secretKeys = []string{"password", "passwd", "secret", "token", "code", "totp", "recovery", "csrf", "cookie", "key"}

// Redact returns a copy of params with values of secret-looking keys replaced by "[redacted]".
func Redact(params map[string]any) map[string]any {
	if params == nil {
		return nil
	}
	out := make(map[string]any, len(params))
	for k, v := range params {
		lk := strings.ToLower(k)
		redacted := false
		for _, s := range secretKeys {
			if strings.Contains(lk, s) {
				redacted = true
				break
			}
		}
		if redacted {
			out[k] = "[redacted]"
		} else {
			out[k] = v
		}
	}
	return out
}

func entryHash(prev []byte, ts int64, e Entry, params string) [32]byte {
	h := sha256.New()
	h.Write(prev)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(ts))
	h.Write(b[:])
	for _, f := range []string{e.Actor, e.Role, e.IP, e.ReqID, e.Action, e.Target, params, e.Result, e.JobID} {
		h.Write([]byte(f))
		h.Write([]byte{0x1f})
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Append adds an entry to the chain and mirrors it to the structured log. It never returns the entry's
// params to callers; errors are returned so security-critical callers can fail closed if they choose.
func (l *Log) Append(ctx context.Context, e Entry) error {
	if e.Action == "" || (e.Result != OK && e.Result != Fail && e.Result != Denied) {
		return fmt.Errorf("audit: invalid entry (action=%q result=%q)", e.Action, e.Result)
	}
	paramsJSON := ""
	if e.Params != nil {
		b, err := json.Marshal(Redact(e.Params))
		if err != nil {
			return fmt.Errorf("audit: params: %w", err)
		}
		paramsJSON = string(b)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ts := l.clk.Now().UnixMilli()
	err := l.db.InTx(ctx, func(tx *sql.Tx) error {
		var prev []byte
		if err := tx.QueryRowContext(ctx, `SELECT hash FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&prev); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		h := entryHash(prev, ts, e, paramsJSON)
		var params any
		if paramsJSON != "" {
			params = paramsJSON
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO audit_log(ts, actor, role, ip, req_id, action, target, params, result, job_id, prev_hash, hash)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			ts, nullable(e.Actor), nullable(e.Role), nullable(e.IP), nullable(e.ReqID), e.Action, nullable(e.Target),
			params, e.Result, nullable(e.JobID), prev, h[:])
		return err
	})
	if err != nil {
		l.log.Error("audit append failed", "action", e.Action, "err", err)
		return err
	}
	l.log.Info("audit", "action", e.Action, "result", e.Result, "actor", e.Actor, "target", e.Target, "ip", e.IP, "reqId", e.ReqID)
	return nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ErrChainBroken is returned by Verify when a row was modified, removed or reordered.
var ErrChainBroken = errors.New("audit: hash chain broken")

// Verify recomputes the chain and returns the number of verified rows.
func (l *Log) Verify(ctx context.Context) (int, error) {
	rows, err := l.db.QueryContext(ctx,
		`SELECT id, ts, actor, role, ip, req_id, action, target, params, result, job_id, prev_hash, hash FROM audit_log ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var prev []byte
	n := 0
	for rows.Next() {
		var (
			id, ts                                        int64
			actor, role, ip, reqID, target, params, jobID sql.NullString
			action, result                                string
			storedPrev, hash                              []byte
		)
		if err := rows.Scan(&id, &ts, &actor, &role, &ip, &reqID, &action, &target, &params, &result, &jobID, &storedPrev, &hash); err != nil {
			return n, err
		}
		if !bytes.Equal(storedPrev, prev) {
			return n, fmt.Errorf("%w: row %d does not link to its predecessor", ErrChainBroken, id)
		}
		e := Entry{Actor: actor.String, Role: role.String, IP: ip.String, ReqID: reqID.String, Action: action, Target: target.String, Result: result, JobID: jobID.String}
		want := entryHash(prev, ts, e, params.String)
		if !bytes.Equal(want[:], hash) {
			return n, fmt.Errorf("%w: row %d content was modified", ErrChainBroken, id)
		}
		prev = hash
		n++
	}
	return n, rows.Err()
}
