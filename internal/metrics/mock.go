package metrics

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"
)

// Mock is the deterministic demo provider: a dual-socket server with two GPUs, three interfaces and four disks.
// Values depend only on (seed, round number), never on wall-clock time, so screenshots and tests are reproducible.
type Mock struct {
	seed uint64
	n    int
}

// NewMock creates the mock; use the same seed (--demo-seed) for the same data.
func NewMock(seed int64) *Mock { return &Mock{seed: uint64(seed)} }

func (m *Mock) Name() string            { return "mock" }
func (m *Mock) Interval() time.Duration { return time.Second }

const (
	mockSockets = 2
	mockThreads = 16 // logical CPUs per socket
	mockRAM     = 64 << 30
)

func (m *Mock) Devices(context.Context) []Device {
	var out []Device
	for p := 0; p < mockSockets; p++ {
		logical := make([]int, mockThreads)
		for i := range logical {
			logical[i] = p*mockThreads + i
		}
		out = append(out, Device{Kind: "cpu", ID: strconv.Itoa(p), Label: "Intel(R) Xeon(R) Silver 4314 CPU @ 2.40GHz",
			Props: map[string]any{"cores": mockThreads / 2, "threads": mockThreads, "logical": logical}})
	}
	for _, n := range []struct {
		id     string
		speed  int64
		hidden bool
	}{{"eth0", 10_000_000_000, false}, {"eth1", 1_000_000_000, false}, {"docker0", 0, true}} {
		props := map[string]any{"up": true}
		if n.speed > 0 {
			props["speedBitsPerSec"] = n.speed
		}
		out = append(out, Device{Kind: "net", ID: n.id, Label: n.id, Hidden: n.hidden, Props: props})
	}
	for i := 0; i < 2; i++ {
		out = append(out, Device{Kind: "gpu", ID: strconv.Itoa(i), Label: "NVIDIA GeForce RTX 3090", Props: map[string]any{"vendor": "nvidia"}})
	}
	for _, d := range []struct {
		id, model, typ string
		cap            int64
	}{
		{"sda", "WDC WD140EFGX-68B", "hdd", 14_000_519_643_136}, {"sdb", "WDC WD140EFGX-68B", "hdd", 14_000_519_643_136},
		{"sdc", "Samsung SSD 870 EVO", "ssd", 1_000_204_886_016}, {"nvme0n1", "Samsung SSD 980 PRO 1TB", "nvme", 1_000_204_886_016},
	} {
		out = append(out, Device{Kind: "disk", ID: d.id, Label: d.model, Props: map[string]any{"type": d.typ, "capacityBytes": d.cap}})
	}
	return out
}

// splitmix64 → [0,1)
func (m *Mock) noise(a, b int) float64 {
	z := m.seed + uint64(a)*0x9E3779B97F4A7C15 + uint64(b)*0xBF58476D1CE4E5B9
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	z ^= z >> 31
	return float64(z>>11) / float64(1<<53)
}

// wave is a slow oscillation in [0,1] with its own period and phase per series index.
func (m *Mock) wave(idx int, period float64) float64 {
	phase := m.noise(idx, -1) * 2 * math.Pi
	return 0.5 + 0.5*math.Sin(2*math.Pi*float64(m.n)/period+phase)
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func (m *Mock) Sample(_ context.Context, _ time.Time) ([]Sample, error) {
	m.n++
	var out []Sample
	add := func(key, label, unit, group, dev string, v float64) {
		out = append(out, Sample{Meta{key, label, unit, group, dev}, v})
	}
	// CPU: per logical CPU a bursty load; package = mean
	for p := 0; p < mockSockets; p++ {
		id := strconv.Itoa(p)
		sum := 0.0
		for i := 0; i < mockThreads; i++ {
			l := p*mockThreads + i
			u := clamp(8+55*m.wave(l, 40+float64(l%7)*9)+18*(m.noise(l, m.n)-0.5)*2, 0, 100)
			if (l+m.n/25)%11 == 0 { // an occasional busy thread
				u = clamp(u+40, 0, 100)
			}
			add(fmt.Sprintf("cpu.core.%d.util", l), fmt.Sprintf("CPU %d", l), "percent", "cpu", id, u)
			sum += u
		}
		avg := sum / mockThreads
		add("cpu."+id+".util", "Utilisation", "percent", "cpu", id, avg)
		add("cpu."+id+".temp", "Temperature", "celsius", "cpu", id, 38+avg*0.38+m.noise(p, m.n))
		add("cpu."+id+".freq", "Frequency", "hertz", "cpu", id, 1.2e9+avg/100*1.6e9)
		add("cpu."+id+".power", "Package power", "watt", "cpu", id, 28+avg*0.9+m.noise(p+9, m.n)*3)
	}
	// Memory
	arc := 18.0*(1<<30) + 2.0*(1<<30)*m.wave(200, 300)
	cache := 6.0*(1<<30) + 1.5*(1<<30)*m.wave(201, 180)
	used := 14.0*(1<<30) + 3.0*(1<<30)*m.wave(202, 220)
	add("mem.total", "Total", "bytes", "mem", "", mockRAM)
	add("mem.used", "Used", "bytes", "mem", "", used)
	add("mem.cache", "Cache/Buffers", "bytes", "mem", "", cache)
	add("mem.arc", "ZFS ARC", "bytes", "mem", "", arc)
	add("mem.free", "Free", "bytes", "mem", "", mockRAM-used-cache-arc)
	add("mem.swap.used", "Swap used", "bytes", "mem", "", 256<<20)
	// Network
	for i, n := range []struct {
		id    string
		speed float64
	}{{"eth0", 10e9}, {"eth1", 1e9}, {"docker0", 0}} {
		peak := 40e6 + float64(i)*10e6
		if n.speed > 0 {
			peak = n.speed / 8 * 0.04
		}
		rx := peak * (0.1 + 0.9*m.wave(300+i, 70)) * (0.7 + 0.6*m.noise(300+i, m.n))
		tx := peak * 0.35 * (0.1 + 0.9*m.wave(310+i, 90)) * (0.7 + 0.6*m.noise(310+i, m.n))
		add("net."+n.id+".rx", "Download", "bytes/s", "net", n.id, rx)
		add("net."+n.id+".tx", "Upload", "bytes/s", "net", n.id, tx)
		add("net."+n.id+".errors", "Errors and drops", "count", "net", n.id, 0)
		if n.speed > 0 {
			add("net."+n.id+".speed", "Link speed", "bit/s", "net", n.id, n.speed)
		}
	}
	// GPUs
	for g := 0; g < 2; g++ {
		id := strconv.Itoa(g)
		u := clamp(75*m.wave(400+g, 120+float64(g)*35)+5*m.noise(400+g, m.n), 0, 100)
		if g == 1 {
			u *= 0.35
		}
		add("gpu."+id+".util", "Utilisation", "percent", "gpu", id, u)
		add("gpu."+id+".temp", "Temperature", "celsius", "gpu", id, 34+u*0.42)
		add("gpu."+id+".vram.used", "VRAM used", "bytes", "gpu", id, (2+14*u/100)*(1<<30))
		add("gpu."+id+".vram.total", "VRAM total", "bytes", "gpu", id, 24<<30)
		add("gpu."+id+".power", "Power", "watt", "gpu", id, 30+u*3.0)
		add("gpu."+id+".power.limit", "Power limit", "watt", "gpu", id, 350)
		add("gpu."+id+".fan", "Fan", "percent", "gpu", id, 30+u*0.5)
	}
	// Disks (these change every second here although the real provider samples every 2 s)
	for i, d := range []string{"sda", "sdb", "sdc", "nvme0n1"} {
		scale := []float64{90e6, 85e6, 280e6, 1200e6}[i]
		busy := clamp(100*m.wave(500+i, 60+float64(i)*13)*0.6+10*m.noise(500+i, m.n), 0, 100)
		add("disk."+d+".read", "Read", "bytes/s", "disk", d, scale*busy/100*0.7)
		add("disk."+d+".write", "Write", "bytes/s", "disk", d, scale*busy/100*0.3)
		add("disk."+d+".iops", "IOPS", "count/s", "disk", d, busy*[]float64{1.8, 1.7, 40, 120}[i])
		add("disk."+d+".util", "Busy", "percent", "disk", d, busy)
		add("disk."+d+".temp", "Temperature", "celsius", "disk", d, []float64{34, 35, 31, 41}[i]+busy*0.08)
	}
	add("sys.load1", "Load 1 min", "load", "sys", "", 2+6*m.wave(600, 200))
	add("sys.load5", "Load 5 min", "load", "sys", "", 3+4*m.wave(601, 400))
	add("sys.load15", "Load 15 min", "load", "sys", "", 3.5+2*m.wave(602, 800))
	add("sys.uptime", "Uptime", "seconds", "sys", "", 259200+float64(m.n))
	return out, nil
}
