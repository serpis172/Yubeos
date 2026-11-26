package metrics

import (
	"context"
	"database/sql"
	"sort"
	"strings"
)

// agg is the running min/avg/max of one series inside the current minute.
type agg struct {
	min, max, sum float64
	n             int
}

func (a *agg) add(v float64) {
	if a.n == 0 || v < a.min {
		a.min = v
	}
	if a.n == 0 || v > a.max {
		a.max = v
	}
	a.sum += v
	a.n++
}

// Rollups persists one aggregated row per series and minute and answers long-range history queries.
type Rollups interface {
	// Write stores the minute starting at unix second ts (idempotent) and drops rows older than keepAfter.
	Write(ctx context.Context, ts int64, rows map[string]agg, keepAfter int64) error
	// Query returns the average per step-second bucket since from for series matching the patterns.
	Query(ctx context.Context, patterns []string, from int64, step int) (map[string]Points, error)
}

// SQLRollups is the SQLite implementation (table metrics_rollup, migration 0002).
type SQLRollups struct{ DB *sql.DB }

func (s SQLRollups) Write(ctx context.Context, ts int64, rows map[string]agg, keepAfter int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	st, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO metrics_rollup(series, ts_min, min, avg, max) VALUES(?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	for k, a := range rows {
		if _, err := st.ExecContext(ctx, k, ts, a.min, a.sum/float64(a.n), a.max); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM metrics_rollup WHERE ts_min < ?`, keepAfter); err != nil {
		return err
	}
	return tx.Commit()
}

// likeEscape makes a literal prefix safe for LIKE ... ESCAPE '\'.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (s SQLRollups) Query(ctx context.Context, patterns []string, from int64, step int) (map[string]Points, error) {
	var conds []string
	args := []any{step, step}
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if strings.HasSuffix(p, ".*") {
			conds = append(conds, `series LIKE ? ESCAPE '\'`)
			args = append(args, likeEscape(p[:len(p)-1])+"%")
		} else if p != "" {
			conds = append(conds, `series = ?`)
			args = append(args, p)
		}
	}
	out := map[string]Points{}
	if len(conds) == 0 {
		return out, nil
	}
	args = append(args, from)
	q := `SELECT series, ts_min/?*? AS b, AVG(avg) FROM metrics_rollup WHERE (` + strings.Join(conds, " OR ") +
		`) AND ts_min >= ? GROUP BY series, b ORDER BY series, b`
	rows, err := s.DB.QueryContext(ctx, q, args...) // args order: bucket step twice, series conditions, from
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key string
		var b int64
		var v float64
		if err := rows.Scan(&key, &b, &v); err != nil {
			return nil, err
		}
		p := out[key]
		p.T, p.V = append(p.T, float64(b*1000)), append(p.V, v)
		out[key] = p
	}
	return out, rows.Err()
}

// sortedKeys is a small helper for tests and stable output.
func sortedKeys(m map[string]Points) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}
