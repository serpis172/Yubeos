package metrics

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/serpis172/Yubeos/internal/db"
)

func TestRollupsPersistAndAnswerLongRanges(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "y.db"), db.Migrations(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	h := NewHub(&fake{})
	h.SetStore(SQLRollups{DB: d.DB})
	now := time.Unix(1_700_000_040, 0) // 20 s before a minute boundary
	h.now = func() time.Time { return now }
	for i := 0; i < 200; i++ { // 200 s = three minute boundaries
		h.Tick()
		now = now.Add(time.Second)
	}
	var n int
	if err := d.DB.QueryRow(`SELECT COUNT(*) FROM metrics_rollup WHERE series='cpu.0.util'`).Scan(&n); err != nil || n < 3 {
		t.Fatalf("rollup rows for cpu.0.util = %d (%v), want at least 3 completed minutes", n, err)
	}
	var mn, avg, mx float64
	if err := d.DB.QueryRow(`SELECT min, avg, max FROM metrics_rollup WHERE series='cpu.0.util' ORDER BY ts_min LIMIT 1`).Scan(&mn, &avg, &mx); err != nil || !(mn <= avg && avg <= mx) {
		t.Fatalf("min/avg/max = %v %v %v (%v)", mn, avg, mx, err)
	}

	hist, err := h.History(ctx, []string{"cpu.*"}, "24h", 0)
	if err != nil {
		t.Fatal(err)
	}
	if hist.StepSeconds != 60 || len(hist.Series["cpu.0.util"].V) < 3 {
		t.Fatalf("24h history: step=%v keys=%v points=%d", hist.StepSeconds, sortedKeys(hist.Series), len(hist.Series["cpu.0.util"].V))
	}
	if _, ok := hist.Series["mem.used"]; ok {
		t.Fatal("pattern cpu.* must not match mem.used")
	}
	hist, _ = h.History(ctx, []string{"cpu.0.util"}, "7d", 300) // coarser buckets: fewer points
	if got := len(hist.Series["cpu.0.util"].V); got < 1 || got > 2 {
		t.Fatalf("7d/300s points = %d", got)
	}

	// pruning: rows older than the retention window are dropped on the next write
	if _, err := d.DB.Exec(`INSERT INTO metrics_rollup VALUES('old.series', ?, 1,1,1)`, now.Add(-31*24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 70; i++ {
		h.Tick()
		now = now.Add(time.Second)
	}
	if err := d.DB.QueryRow(`SELECT COUNT(*) FROM metrics_rollup WHERE series='old.series'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("old row survived pruning: %d %v", n, err)
	}
	if c := h.Catalog(); len(c.Providers) != 2 || !c.Providers[1].OK {
		t.Fatalf("providers: %+v", c.Providers)
	}
}

func TestLikeEscape(t *testing.T) {
	if got := likeEscape(`net.eth_0%\`); got != `net.eth\_0\%\\` {
		t.Fatalf("got %q", got)
	}
}
