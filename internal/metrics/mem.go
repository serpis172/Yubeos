package metrics

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

// parseMeminfo returns /proc/meminfo values in bytes.
func parseMeminfo(s string) map[string]uint64 {
	out := map[string]uint64{}
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		n, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			continue
		}
		if len(f) > 1 && strings.EqualFold(f[1], "kB") {
			n *= 1024
		}
		out[k] = n
	}
	return out
}

// parseARC returns the ZFS ARC size in bytes from /proc/spl/kstat/zfs/arcstats.
func parseARC(s string) (uint64, bool) {
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == "size" {
			n, err := strconv.ParseUint(f[2], 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}

// Memory reports RAM with the ZFS ARC separated from the page cache (ARC lives outside it).
type Memory struct{ fs FS }

func NewMemory(fs FS) *Memory { return &Memory{fs: fs} }

func (m *Memory) Name() string                     { return "mem" }
func (m *Memory) Interval() time.Duration          { return time.Second }
func (m *Memory) Devices(context.Context) []Device { return nil }

func (m *Memory) Sample(_ context.Context, _ time.Time) ([]Sample, error) {
	s, err := m.fs.Read("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	mi := parseMeminfo(s)
	total := mi["MemTotal"]
	if total == 0 {
		return nil, errors.New("MemTotal missing in /proc/meminfo")
	}
	free := mi["MemFree"]
	cache := mi["Buffers"] + mi["Cached"] + mi["SReclaimable"]
	if sh := mi["Shmem"]; cache > sh {
		cache -= sh // shmem is counted inside Cached but cannot be reclaimed like page cache
	}
	var arc uint64
	hasARC := false
	if a, err := m.fs.Read("/proc/spl/kstat/zfs/arcstats"); err == nil {
		arc, hasARC = parseARC(a)
	}
	used := int64(total) - int64(free) - int64(cache) - int64(arc)
	if used < 0 {
		used = 0
	}
	g := func(k, label string) Meta { return Meta{"mem." + k, label, "bytes", "mem", ""} }
	out := []Sample{
		{g("total", "Total"), float64(total)},
		{g("used", "Used"), float64(used)},
		{g("free", "Free"), float64(free)},
		{g("cache", "Cache/Buffers"), float64(cache)},
	}
	if hasARC {
		out = append(out, Sample{g("arc", "ZFS ARC"), float64(arc)})
	}
	if st := mi["SwapTotal"]; st > 0 {
		out = append(out, Sample{g("swap.used", "Swap used"), float64(st - mi["SwapFree"])})
	}
	return out, nil
}

// System reports load averages and uptime.
type System struct{ fs FS }

func NewSystem(fs FS) *System { return &System{fs: fs} }

func (s *System) Name() string                     { return "sys" }
func (s *System) Interval() time.Duration          { return 10 * time.Second }
func (s *System) Devices(context.Context) []Device { return nil }

func (s *System) Sample(_ context.Context, _ time.Time) ([]Sample, error) {
	la, err := s.fs.Read("/proc/loadavg")
	if err != nil {
		return nil, err
	}
	f := strings.Fields(la)
	if len(f) < 3 {
		return nil, errors.New("malformed /proc/loadavg")
	}
	var out []Sample
	for i, k := range []string{"load1", "load5", "load15"} {
		v, err := strconv.ParseFloat(f[i], 64)
		if err != nil {
			return nil, err
		}
		out = append(out, Sample{Meta{"sys." + k, "Load " + strings.TrimPrefix(k, "load") + " min", "load", "sys", ""}, v})
	}
	if up := strings.Fields(s.fs.ReadTrim("/proc/uptime")); len(up) > 0 {
		if v, err := strconv.ParseFloat(up[0], 64); err == nil {
			out = append(out, Sample{Meta{"sys.uptime", "Uptime", "seconds", "sys", ""}, v})
		}
	}
	return out, nil
}
