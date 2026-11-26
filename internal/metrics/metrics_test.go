package metrics

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func put(t *testing.T, root, rel, data string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func near(t *testing.T, got map[string]float64, key string, want float64) {
	t.Helper()
	v, ok := got[key]
	if !ok || math.Abs(v-want) > 0.01 {
		t.Fatalf("%s = %v (present=%v), want %v", key, v, ok, want)
	}
}

func TestProcComputesRatesFromTwoSamples(t *testing.T) {
	root := t.TempDir()
	proc, sys := filepath.Join(root, "proc"), filepath.Join(root, "sys")
	files := func(user, idle, rx, tx, sectorsRead, busy string) {
		put(t, proc, "stat", "cpu  "+user+" 0 0 "+idle+" 0 0 0 0\ncpu0 "+user+" 0 0 "+idle+" 0 0 0 0\n")
		put(t, proc, "net/dev", "Inter-|   Receive\n face |bytes\n  lo: 5 0 0 0 0 0 0 0 5 0 0 0 0 0 0 0\n eth0: "+rx+" 0 0 0 0 0 0 0 "+tx+" 0 0 0 0 0 0 0\n")
		put(t, proc, "diskstats", "   8 0 sda 10 0 "+sectorsRead+" 0 10 0 0 0 0 "+busy+" 0\n   8 1 sda1 1 0 1 0 1 0 1 0 0 0 0\n")
	}
	put(t, proc, "meminfo", "MemTotal: 1000 kB\nMemFree: 200 kB\nBuffers: 50 kB\nCached: 250 kB\nSReclaimable: 0 kB\nSwapTotal: 100 kB\nSwapFree: 40 kB\n")
	put(t, proc, "loadavg", "1.50 2.00 2.50 1/100 1234\n")
	put(t, proc, "uptime", "259200.55 1000.00\n")
	put(t, proc, "cpuinfo", "model name\t: Test CPU\ncpu cores\t: 1\ncpu MHz\t\t: 2000.0\n")
	put(t, sys, "block/sda/size", "2048\n")
	put(t, sys, "block/sda/queue/rotational", "1\n")
	put(t, sys, "class/net/eth0/speed", "1000\n")

	p := &Proc{procRoot: proc, sysRoot: sys}
	t0 := time.Unix(1000, 0)
	files("100", "900", "1000", "2000", "0", "0")
	first, err := p.Sample(t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := first["net.eth0.rx"]; has {
		t.Fatal("rates must not appear on the first sample")
	}
	near(t, first, "mem.total", 1000*1024)
	near(t, first, "mem.cache", 300*1024)
	near(t, first, "mem.used", 500*1024)
	near(t, first, "mem.swap.used", 60*1024)
	near(t, first, "sys.load1", 1.5)
	near(t, first, "cpu.0.freq", 2e9)

	files("150", "950", "3000", "2500", "2048", "500") // +50 busy of 100 ticks, +2000 B rx, +500 B tx over 2 s
	second, err := p.Sample(t0.Add(2 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	near(t, second, "cpu.0.util", 50)
	near(t, second, "cpu.core.0.util", 50)
	near(t, second, "net.eth0.rx", 1000)
	near(t, second, "net.eth0.tx", 250)
	near(t, second, "disk.sda.read", 2048*512/2.0)
	near(t, second, "disk.sda.util", 25) // 500 ms busy in 2 s
	if _, has := second["disk.sda1.read"]; has {
		t.Fatal("partitions must be ignored")
	}

	var gotNet, gotDisk, gotCPU bool
	for _, d := range p.Devices() {
		switch d.Kind {
		case "net":
			gotNet = d.ID == "eth0" && d.Props["speedBitsPerSec"] == 1e9 && !d.Hidden
		case "disk":
			gotDisk = d.ID == "sda" && d.Props["type"] == "hdd" && d.Props["capacityBytes"] == 2048.0*512
		case "cpu":
			gotCPU = d.Label == "Test CPU" && d.Props["threads"] == 1
		}
	}
	if !gotNet || !gotDisk || !gotCPU {
		t.Fatalf("devices wrong: %+v", p.Devices())
	}
}

func TestProcWithoutProcfsReportsAProviderProblem(t *testing.T) {
	h := NewHub(&Proc{procRoot: t.TempDir(), sysRoot: t.TempDir()})
	h.Tick()
	c := h.Catalog()
	if len(c.Providers) != 1 || c.Providers[0].OK || c.Providers[0].Reason == "" {
		t.Fatalf("providers: %+v", c.Providers)
	}
}

type fake struct{ n float64 }

func (*fake) Name() string      { return "fake" }
func (*fake) Devices() []Device { return nil }
func (f *fake) Sample(time.Time) (map[string]float64, error) {
	f.n++
	return map[string]float64{"cpu.0.util": f.n, "mem.used": 10 * f.n}, nil
}

func TestHubHistoryPatternsAndBuckets(t *testing.T) {
	h := NewHub(&fake{})
	now := time.Unix(10_000, 0)
	h.now = func() time.Time { return now }
	for i := 0; i < 20; i++ {
		now = now.Add(time.Second)
		h.Tick()
	}
	hist, err := h.History(context.Background(), []string{"cpu.*"}, "5m", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hist.Series["mem.used"]; ok {
		t.Fatal("pattern cpu.* must not match mem.used")
	}
	if got := len(hist.Series["cpu.0.util"].V); got != 20 {
		t.Fatalf("raw points = %d, want 20", got)
	}
	hist, _ = h.History(context.Background(), []string{"cpu.0.util"}, "1h", 10)
	if n := len(hist.Series["cpu.0.util"].V); n < 2 || n > 3 || hist.StepSeconds != 10 {
		t.Fatalf("bucketed points = %d step=%v", n, hist.StepSeconds)
	}
	for _, bad := range [][]string{nil, make([]string, 65)} {
		if _, err := h.History(context.Background(), bad, "5m", 0); err == nil {
			t.Fatalf("expected error for %d patterns", len(bad))
		}
	}
	if _, err := h.History(context.Background(), []string{"cpu.*"}, "2y", 0); err == nil {
		t.Fatal("unknown range must be rejected")
	}
}

func TestHubSnapshotCatalogAndStaleness(t *testing.T) {
	h := NewHub(&fake{})
	now := time.Unix(10_000, 0)
	h.now = func() time.Time { return now }
	h.Tick()
	if s := h.Snapshot(); s.Values["cpu.0.util"] != 1 || len(s.Stale) != 0 {
		t.Fatalf("snapshot: %+v", s)
	}
	now = now.Add(10 * time.Second) // sampler stalled
	if s := h.Snapshot(); len(s.Stale) != 2 {
		t.Fatalf("stale: %+v", s.Stale)
	}
	c := h.Catalog()
	if len(c.Series) != 2 || c.Series[0].Key != "cpu.0.util" || c.Series[0].Unit != "percent" || c.Series[0].Group != "cpu" || !c.Series[0].Stale {
		t.Fatalf("catalog: %+v", c.Series)
	}
}

func TestRingIsBounded(t *testing.T) {
	h := NewHub(&fake{})
	for i := 0; i < ringSize+50; i++ {
		h.Tick()
	}
	if len(h.ring) != ringSize {
		t.Fatalf("ring = %d", len(h.ring))
	}
}

func TestDemoIsDeterministic(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	a, _ := NewDemo(42).Sample(at)
	b, _ := NewDemo(42).Sample(at)
	for k, v := range a {
		if b[k] != v || math.IsNaN(v) || v < 0 {
			t.Fatalf("%s: %v vs %v", k, v, b[k])
		}
	}
	if a["mem.free"] < 0 || a["cpu.0.util"] > 100 {
		t.Fatalf("out of range: %+v", a)
	}
}
