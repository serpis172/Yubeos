// Package metrics samples host statistics once per second into a one-hour in-memory ring and serves the
// catalog / snapshot / history views behind /api/v1/metrics/* (docs/modules/dashboard.md).
//
// Ranges beyond one hour (24 h .. 30 d) are answered from per-minute rollups in SQLite when a Rollups store is
// set (SetStore); without one they fall back to the ring, i.e. at most one hour of data.
package metrics

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	ringSize   = 3600 // one sample per second, one hour
	staleAfter = 5 * time.Second
	maxSeries  = 64
	retention  = 30 * 24 * time.Hour
)

// Source produces one flat set of values per tick, keyed like "cpu.0.util" or "net.eth0.rx".
type Source interface {
	Name() string
	// Devices describes the hardware behind the keys (may change between calls, e.g. interfaces appearing).
	Devices() []Device
	// Sample returns the current values. Rates need two samples: the first call may return fewer keys.
	Sample(now time.Time) (map[string]float64, error)
}

type Series struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Unit   string `json:"unit"`
	Group  string `json:"group"`
	Device string `json:"device"`
	Stale  bool   `json:"stale"`
}

type Device struct {
	Kind   string         `json:"kind"`
	ID     string         `json:"id"`
	Label  string         `json:"label"`
	Hidden bool           `json:"hidden,omitempty"`
	Props  map[string]any `json:"props,omitempty"`
}

type Provider struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

type Catalog struct {
	Series    []Series   `json:"series"`
	Devices   []Device   `json:"devices"`
	Providers []Provider `json:"providers"`
}

type Snapshot struct {
	T      int64              `json:"t"`
	Values map[string]float64 `json:"values"`
	Stale  []string           `json:"stale"`
}

type Points struct {
	T []float64 `json:"t"`
	V []float64 `json:"v"`
}

type History struct {
	Range       string            `json:"range"`
	StepSeconds float64           `json:"stepSeconds"`
	Series      map[string]Points `json:"series"`
}

type frame struct {
	t int64 // unix ms
	v map[string]float64
}

// Hub owns the sampling loop and the ring buffer. The zero value is not usable: call NewHub.
type Hub struct {
	src Source
	now func() time.Time

	mu       sync.RWMutex
	ring     []frame // oldest first, at most ringSize
	known    map[string]bool
	provider Provider

	store     Rollups // optional: minute rollups for ranges beyond the ring
	storeErr  string
	acc       map[string]*agg
	accMinute int64 // unix seconds of the minute being aggregated
}

// SetStore enables minute rollups (24 h .. 30 d history). Call before Run.
func (h *Hub) SetStore(r Rollups) {
	h.mu.Lock()
	h.store = r
	h.mu.Unlock()
}

// NewHub wraps a source. Call Run to start sampling (tests call Tick directly).
func NewHub(src Source) *Hub {
	return &Hub{src: src, now: time.Now, known: map[string]bool{}, provider: Provider{Name: src.Name(), OK: true}}
}

// Run samples every second until ctx is cancelled.
func (h *Hub) Run(ctx context.Context) {
	h.Tick()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.Tick()
		}
	}
}

// Tick takes one sample.
func (h *Hub) Tick() {
	now := h.now()
	v, err := h.src.Sample(now)
	minute, rows, store := h.record(now, v, err)
	if len(rows) == 0 || store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	werr := store.Write(ctx, minute, rows, now.Add(-retention).Unix())
	h.mu.Lock()
	h.storeErr = ""
	if werr != nil {
		h.storeErr = werr.Error()
	}
	h.mu.Unlock()
}

// record appends the sample to the ring and the per-minute accumulator. When a minute has just ended it
// returns that minute's aggregates so the caller can persist them outside the lock.
func (h *Hub) record(now time.Time, v map[string]float64, err error) (int64, map[string]agg, Rollups) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err != nil {
		h.provider = Provider{Name: h.src.Name(), OK: false, Reason: err.Error()}
		return 0, nil, nil
	}
	h.provider = Provider{Name: h.src.Name(), OK: true}
	if len(v) == 0 {
		return 0, nil, nil
	}
	for k := range v {
		h.known[k] = true
	}
	h.ring = append(h.ring, frame{t: now.UnixMilli(), v: v})
	if len(h.ring) > ringSize {
		h.ring = append(h.ring[:0], h.ring[len(h.ring)-ringSize:]...)
	}
	if h.store == nil {
		return 0, nil, nil
	}
	minute := now.Unix() / 60 * 60
	var done map[string]agg
	var doneMinute int64
	if h.acc != nil && minute != h.accMinute {
		done, doneMinute = make(map[string]agg, len(h.acc)), h.accMinute
		for k, a := range h.acc {
			done[k] = *a
		}
		h.acc = nil
	}
	if h.acc == nil {
		h.acc, h.accMinute = map[string]*agg{}, minute
	}
	for k, x := range v {
		a := h.acc[k]
		if a == nil {
			a = &agg{}
			h.acc[k] = a
		}
		a.add(x)
	}
	return doneMinute, done, h.store
}

func (h *Hub) stale(now time.Time) map[string]bool {
	out := map[string]bool{}
	var last *frame
	if n := len(h.ring); n > 0 {
		last = &h.ring[n-1]
	}
	fresh := last != nil && now.Sub(time.UnixMilli(last.t)) <= staleAfter
	for k := range h.known {
		if !fresh || !hasKey(last.v, k) {
			out[k] = true
		}
	}
	return out
}

func hasKey(m map[string]float64, k string) bool { _, ok := m[k]; return ok }

// Snapshot returns the latest value of every series.
func (h *Hub) Snapshot() Snapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	st := h.stale(h.now())
	out := Snapshot{Values: map[string]float64{}, Stale: []string{}}
	if n := len(h.ring); n > 0 {
		out.T = h.ring[n-1].t
		for k, v := range h.ring[n-1].v {
			out.Values[k] = v
		}
	}
	for k := range st {
		out.Stale = append(out.Stale, k)
	}
	sort.Strings(out.Stale)
	return out
}

// Catalog lists every series seen so far, the devices and the provider health.
func (h *Hub) Catalog() Catalog {
	devs := h.src.Devices()
	h.mu.RLock()
	defer h.mu.RUnlock()
	st := h.stale(h.now())
	keys := make([]string, 0, len(h.known))
	for k := range h.known {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	c := Catalog{Series: make([]Series, 0, len(keys)), Devices: devs, Providers: []Provider{h.provider}}
	if h.store != nil {
		c.Providers = append(c.Providers, Provider{Name: "rollups", OK: h.storeErr == "", Reason: h.storeErr})
	}
	if c.Devices == nil {
		c.Devices = []Device{}
	}
	for _, k := range keys {
		s := Describe(k)
		s.Stale = st[k]
		c.Series = append(c.Series, s)
	}
	return c
}

// rangeSeconds maps the API range names to a duration; ok is false for unknown names.
func rangeSeconds(r string) (int, bool) {
	switch r {
	case "5m":
		return 300, true
	case "1h":
		return 3600, true
	case "24h":
		return 86400, true
	case "7d":
		return 7 * 86400, true
	case "30d":
		return 30 * 86400, true
	}
	return 0, false
}

func defaultStep(r string) int {
	switch r {
	case "5m":
		return 0 // raw samples
	case "1h":
		return 10
	case "24h":
		return 60
	case "7d":
		return 600
	}
	return 1800
}

// ErrInvalid marks a bad history query (maps to HTTP 400).
type ErrInvalid string

func (e ErrInvalid) Error() string { return string(e) }

// History returns the requested series for a range, averaged into step-second buckets (0 = raw samples).
// patterns are exact keys or "prefix.*"; at most 64 of them.
func (h *Hub) History(ctx context.Context, patterns []string, rng string, step int) (History, error) {
	secs, ok := rangeSeconds(rng)
	if !ok {
		return History{}, ErrInvalid("range must be one of 5m, 1h, 24h, 7d, 30d")
	}
	if len(patterns) == 0 || len(patterns) > maxSeries {
		return History{}, ErrInvalid("series needs between 1 and 64 keys or patterns")
	}
	if step < 0 {
		return History{}, ErrInvalid("step must not be negative")
	}
	if step == 0 {
		step = defaultStep(rng)
	}
	h.mu.RLock()
	store := h.store
	h.mu.RUnlock()
	out := History{Range: rng, StepSeconds: float64(step), Series: map[string]Points{}}
	if secs > ringSize && store != nil { // beyond the ring: minute rollups, bucketed in SQL (never finer than a minute)
		if step < 60 {
			step = 60
			out.StepSeconds = 60
		}
		from := h.now().Unix() - int64(secs)
		pts, err := store.Query(ctx, patterns, from, step)
		if err != nil {
			return History{}, err
		}
		out.Series = pts
		return out, nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	cutoff := h.now().Add(-time.Duration(secs) * time.Second).UnixMilli()
	start := sort.Search(len(h.ring), func(i int) bool { return h.ring[i].t >= cutoff })
	frames := h.ring[start:]
	for k := range h.known {
		if !Matches(patterns, k) {
			continue
		}
		out.Series[k] = bucket(frames, k, step)
	}
	return out, nil
}

// Matches reports whether key equals one of the patterns or falls under a "prefix.*" pattern.
func Matches(patterns []string, key string) bool {
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if strings.HasSuffix(p, ".*") {
			if strings.HasPrefix(key, p[:len(p)-1]) {
				return true
			}
		} else if p == key {
			return true
		}
	}
	return false
}

func bucket(frames []frame, key string, step int) Points {
	pts := Points{T: []float64{}, V: []float64{}}
	var sum float64
	var n int
	var bucketStart int64 = -1
	flush := func() {
		if n > 0 {
			pts.T = append(pts.T, float64(bucketStart))
			pts.V = append(pts.V, sum/float64(n))
		}
		sum, n = 0, 0
	}
	for _, f := range frames {
		v, ok := f.v[key]
		if !ok {
			continue
		}
		if step == 0 {
			pts.T = append(pts.T, float64(f.t))
			pts.V = append(pts.V, v)
			continue
		}
		b := f.t / 1000 / int64(step) * int64(step) * 1000
		if b != bucketStart {
			flush()
			bucketStart = b
		}
		sum += v
		n++
	}
	if step != 0 {
		flush()
	}
	return pts
}

// Describe derives label, unit, group and device from a series key.
func Describe(key string) Series {
	parts := strings.Split(key, ".")
	s := Series{Key: key, Label: key, Unit: "count", Group: "sys"}
	switch parts[0] {
	case "cpu":
		s.Group = "cpu"
		if len(parts) >= 3 && parts[1] == "core" {
			s.Device = parts[2]
			s.Label, s.Unit = "Thread "+parts[2]+" load", "percent"
			return s
		}
		if len(parts) >= 3 {
			s.Device = parts[1]
			switch parts[2] {
			case "util":
				s.Label, s.Unit = "CPU load", "percent"
			case "temp":
				s.Label, s.Unit = "CPU temperature", "celsius"
			case "freq":
				s.Label, s.Unit = "CPU frequency", "hertz"
			}
		}
	case "mem":
		s.Group = "mem"
		s.Label, s.Unit = "Memory "+strings.Join(parts[1:], " "), "bytes"
	case "net":
		s.Group = "net"
		if len(parts) >= 3 {
			s.Device = parts[1]
			switch parts[2] {
			case "rx":
				s.Label, s.Unit = parts[1]+" download", "bytes/s"
			case "tx":
				s.Label, s.Unit = parts[1]+" upload", "bytes/s"
			case "errors":
				s.Label = parts[1] + " errors"
			case "speed":
				s.Label, s.Unit = parts[1]+" link speed", "bit/s"
			}
		}
	case "gpu":
		s.Group = "gpu"
		if len(parts) >= 3 {
			s.Device = parts[1]
			s.Label = "GPU " + strings.Join(parts[2:], " ")
			switch parts[2] {
			case "util":
				s.Unit = "percent"
			case "temp":
				s.Unit = "celsius"
			case "vram":
				s.Unit = "bytes"
			case "power":
				s.Unit = "watt"
			}
		}
	case "disk":
		s.Group = "disk"
		if len(parts) >= 3 {
			s.Device = parts[1]
			s.Label = parts[1] + " " + parts[2]
			switch parts[2] {
			case "read", "write":
				s.Unit = "bytes/s"
			case "iops":
				s.Unit = "count/s"
			case "util":
				s.Unit = "percent"
			case "temp":
				s.Unit = "celsius"
			}
		}
	case "sys":
		s.Label = "System " + strings.Join(parts[1:], " ")
		if len(parts) == 2 {
			switch {
			case strings.HasPrefix(parts[1], "load"):
				s.Unit = "load"
			case parts[1] == "uptime":
				s.Unit = "seconds"
			}
		}
	}
	return s
}
