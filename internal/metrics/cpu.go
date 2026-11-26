package metrics

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type cpuTimes struct{ idle, total uint64 }

// parseProcStat parses the per-logical-CPU lines of /proc/stat (the aggregate "cpu " line is skipped).
func parseProcStat(s string) map[int]cpuTimes {
	out := map[int]cpuTimes{}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || !strings.HasPrefix(f[0], "cpu") || len(f[0]) == 3 {
			continue
		}
		n, err := strconv.Atoi(f[0][3:])
		if err != nil {
			continue
		}
		var v [8]uint64
		for i := 0; i < 8 && i+1 < len(f); i++ {
			v[i], _ = strconv.ParseUint(f[i+1], 10, 64)
		}
		// user nice system idle iowait irq softirq steal (guest time is already inside user/nice)
		var total uint64
		for _, x := range v {
			total += x
		}
		out[n] = cpuTimes{idle: v[3] + v[4], total: total}
	}
	return out
}

func utilBetween(prev, cur cpuTimes) (float64, bool) {
	if cur.total <= prev.total {
		return 0, false
	}
	dt, di := float64(cur.total-prev.total), float64(cur.idle-prev.idle)
	u := (1 - di/dt) * 100
	if u < 0 {
		u = 0
	}
	if u > 100 {
		u = 100
	}
	return u, true
}

// parseCPUInfo maps physical id → model name from /proc/cpuinfo (x86 layout; ARM falls back to "Processor"/"Hardware").
func parseCPUInfo(s string) map[int]string {
	out := map[int]string{}
	phys, model := 0, ""
	flush := func() {
		if model != "" {
			if _, ok := out[phys]; !ok {
				out[phys] = model
			}
		}
	}
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			if strings.TrimSpace(line) == "" {
				flush()
				phys, model = 0, ""
			}
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "physical id":
			phys, _ = strconv.Atoi(v)
		case "model name", "Model Name", "Processor", "Hardware":
			if model == "" {
				model = v
			}
		}
	}
	flush()
	return out
}

type cpuTopo struct {
	logical int
	pkg     int
	core    int
}

// CPU reports utilisation per logical CPU and per physical package, plus temperature and frequency.
type CPU struct {
	fs     FS
	prev   map[int]cpuTimes
	primed bool
}

func NewCPU(fs FS) *CPU { return &CPU{fs: fs} }

func (c *CPU) Name() string            { return "cpu" }
func (c *CPU) Interval() time.Duration { return time.Second }

func (c *CPU) topology(logical []int) []cpuTopo {
	t := make([]cpuTopo, 0, len(logical))
	for _, n := range logical {
		base := fmt.Sprintf("/sys/devices/system/cpu/cpu%d/topology/", n)
		pkg, ok := c.fs.ReadInt(base + "physical_package_id")
		if !ok || pkg < 0 {
			pkg = 0
		}
		core, ok := c.fs.ReadInt(base + "core_id")
		if !ok {
			core = int64(n)
		}
		t = append(t, cpuTopo{logical: n, pkg: int(pkg), core: int(core)})
	}
	return t
}

func sortedKeys[V any](m map[int]V) []int {
	k := make([]int, 0, len(m))
	for n := range m {
		k = append(k, n)
	}
	sort.Ints(k)
	return k
}

func (c *CPU) Devices(context.Context) []Device {
	stat, err := c.fs.Read("/proc/stat")
	if err != nil {
		return nil
	}
	topo := c.topology(sortedKeys(parseProcStat(stat)))
	models := parseCPUInfo(c.fs.ReadTrim("/proc/cpuinfo"))
	byPkg := map[int][]cpuTopo{}
	for _, t := range topo {
		byPkg[t.pkg] = append(byPkg[t.pkg], t)
	}
	var out []Device
	for _, pkg := range sortedKeys(byPkg) {
		cores := map[int]bool{}
		logical := []int{}
		for _, t := range byPkg[pkg] {
			cores[t.core] = true
			logical = append(logical, t.logical)
		}
		label := models[pkg]
		if label == "" {
			label = "CPU " + strconv.Itoa(pkg)
		}
		out = append(out, Device{Kind: "cpu", ID: strconv.Itoa(pkg), Label: label, Props: map[string]any{"cores": len(cores), "threads": len(logical), "logical": logical}})
	}
	return out
}

func (c *CPU) Sample(_ context.Context, _ time.Time) ([]Sample, error) {
	stat, err := c.fs.Read("/proc/stat")
	if err != nil {
		return nil, err
	}
	cur := parseProcStat(stat)
	if len(cur) == 0 {
		return nil, errors.New("no CPUs in /proc/stat")
	}
	prev := c.prev
	c.prev = cur
	if prev == nil {
		return nil, nil // first round only primes the counters
	}
	topo := c.topology(sortedKeys(cur))
	var out []Sample
	pkgSum, pkgN := map[int]float64{}, map[int]int{}
	freqSum, freqN := map[int]float64{}, map[int]int{}
	for _, t := range topo {
		u, ok := utilBetween(prev[t.logical], cur[t.logical])
		if !ok {
			continue
		}
		out = append(out, Sample{Meta{fmt.Sprintf("cpu.core.%d.util", t.logical), fmt.Sprintf("CPU %d", t.logical), "percent", "cpu", strconv.Itoa(t.pkg)}, u})
		pkgSum[t.pkg] += u
		pkgN[t.pkg]++
		if khz, ok := c.fs.ReadInt(fmt.Sprintf("/sys/devices/system/cpu/cpu%d/cpufreq/scaling_cur_freq", t.logical)); ok {
			freqSum[t.pkg] += float64(khz) * 1000
			freqN[t.pkg]++
		}
	}
	temps := c.temps()
	for _, pkg := range sortedKeys(pkgN) {
		id := strconv.Itoa(pkg)
		out = append(out, Sample{Meta{"cpu." + id + ".util", "Utilisation", "percent", "cpu", id}, pkgSum[pkg] / float64(pkgN[pkg])})
		if freqN[pkg] > 0 {
			out = append(out, Sample{Meta{"cpu." + id + ".freq", "Frequency", "hertz", "cpu", id}, freqSum[pkg] / float64(freqN[pkg])})
		}
		if t, ok := temps[pkg]; ok {
			out = append(out, Sample{Meta{"cpu." + id + ".temp", "Temperature", "celsius", "cpu", id}, t})
		}
	}
	return out, nil
}

// temps reads package temperatures: coretemp "Package id N" and AMD k10temp Tctl/Tdie (single package).
func (c *CPU) temps() map[int]float64 {
	out := map[int]float64{}
	for _, h := range c.fs.Dir("/sys/class/hwmon") {
		base := "/sys/class/hwmon/" + h + "/"
		switch c.fs.ReadTrim(base + "name") {
		case "coretemp":
			for _, f := range c.fs.Dir(base) {
				if !strings.HasPrefix(f, "temp") || !strings.HasSuffix(f, "_label") {
					continue
				}
				label := c.fs.ReadTrim(base + f)
				if !strings.HasPrefix(label, "Package id ") {
					continue
				}
				pkg, err := strconv.Atoi(strings.TrimPrefix(label, "Package id "))
				if err != nil {
					continue
				}
				if v, ok := c.fs.ReadInt(base + strings.TrimSuffix(f, "_label") + "_input"); ok {
					out[pkg] = float64(v) / 1000
				}
			}
		case "k10temp":
			for _, f := range c.fs.Dir(base) {
				if !strings.HasPrefix(f, "temp") || !strings.HasSuffix(f, "_label") {
					continue
				}
				if l := c.fs.ReadTrim(base + f); l == "Tctl" || l == "Tdie" {
					if v, ok := c.fs.ReadInt(base + strings.TrimSuffix(f, "_label") + "_input"); ok {
						if _, have := out[0]; !have || l == "Tdie" {
							out[0] = float64(v) / 1000
						}
					}
				}
			}
		}
	}
	return out
}
