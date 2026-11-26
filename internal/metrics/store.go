// Package metrics collects host metrics (CPU, memory, network, GPU, disks), keeps the last hour in ring buffers and
// publishes live frames (docs/modules/dashboard.md). Every hardware source is a Provider with a real Linux
// implementation and a deterministic mock (demo mode).
package metrics

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	RingSize    = 3600 // one hour at 1 Hz
	StaleAfter  = 5 * time.Second
	subscriberQ = 8
)

// Meta describes a series (GET /metrics/catalog).
type Meta struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Unit   string `json:"unit"`   // percent | celsius | bytes | bytes/s | bit/s | watt | hertz | count | count/s | load | seconds
	Group  string `json:"group"`  // cpu | mem | net | gpu | disk | sys
	Device string `json:"device"` // owning device id within the group ("" for host-wide series)
}

// Sample is one reading of one series.
type Sample struct {
	Meta
	V float64
}

// Device is static information about a piece of hardware (cards on the dashboard).
type Device struct {
	Kind   string         `json:"kind"` // cpu | net | gpu | disk
	ID     string         `json:"id"`
	Label  string         `json:"label"`
	Hidden bool           `json:"hidden,omitempty"` // virtual interfaces etc.: collected but not shown by default
	Props  map[string]any `json:"props,omitempty"`
}

// ProviderStatus tells the UI why a card is empty ("RAPL requires the privileged helper").
type ProviderStatus struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// Frame is what the WebSocket publishes: values of one collection round.
type Frame struct {
	T int64              `json:"t"` // unix ms
	V map[string]float64 `json:"v"`
}

type ring struct {
	ts   [RingSize]int64
	val  [RingSize]float32
	n    int // number of valid points (≤ RingSize)
	next int // write position
}

func (r *ring) put(t int64, v float64) {
	r.ts[r.next], r.val[r.next] = t, float32(v)
	r.next = (r.next + 1) % RingSize
	if r.n < RingSize {
		r.n++
	}
}

// each calls fn for the points with t in [from, to] in chronological order.
func (r *ring) each(from, to int64, fn func(t int64, v float64)) {
	start := (r.next - r.n + RingSize) % RingSize
	for i := 0; i < r.n; i++ {
		j := (start + i) % RingSize
		if r.ts[j] >= from && r.ts[j] <= to {
			fn(r.ts[j], float64(r.val[j]))
		}
	}
}

type series struct {
	meta Meta
	ring ring
	last float64
	at   time.Time
}

// Store holds the series, the device list, provider statuses and the subscribers of live frames.
type Store struct {
	mu        sync.RWMutex
	series    map[string]*series
	devices   map[string][]Device // by provider name
	status    map[string]ProviderStatus
	now       func() time.Time
	subMu     sync.Mutex
	subs      map[*Subscription]struct{}
	collected uint64
}

// NewStore creates an empty store; now is time.Now in production, a fake clock in tests.
func NewStore(now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{series: map[string]*series{}, devices: map[string][]Device{}, status: map[string]ProviderStatus{}, now: now, subs: map[*Subscription]struct{}{}}
}

// Put records a batch of samples taken at t and publishes a frame.
func (s *Store) Put(t time.Time, samples []Sample) {
	if len(samples) == 0 {
		return
	}
	ms := t.UnixMilli()
	frame := Frame{T: ms, V: make(map[string]float64, len(samples))}
	s.mu.Lock()
	for _, sm := range samples {
		sr, ok := s.series[sm.Key]
		if !ok {
			sr = &series{meta: sm.Meta}
			s.series[sm.Key] = sr
		}
		sr.ring.put(ms, sm.V)
		sr.last, sr.at = sm.V, t
		frame.V[sm.Key] = sm.V
	}
	s.collected++
	s.mu.Unlock()
	s.publish(frame)
}

// SetDevices replaces the devices of one provider.
func (s *Store) SetDevices(provider string, d []Device) {
	s.mu.Lock()
	s.devices[provider] = d
	s.mu.Unlock()
}

// SetStatus records the health of a provider.
func (s *Store) SetStatus(st ProviderStatus) {
	s.mu.Lock()
	s.status[st.Name] = st
	s.mu.Unlock()
}

// CatalogEntry is a series with its freshness.
type CatalogEntry struct {
	Meta
	Stale bool `json:"stale"`
}

// Catalog lists every series ever seen (sorted by key), all devices and provider statuses.
func (s *Store) Catalog() (series []CatalogEntry, devices []Device, providers []ProviderStatus) {
	now := s.now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	series = make([]CatalogEntry, 0, len(s.series))
	for _, sr := range s.series {
		series = append(series, CatalogEntry{Meta: sr.meta, Stale: now.Sub(sr.at) > StaleAfter})
	}
	sort.Slice(series, func(i, j int) bool { return series[i].Key < series[j].Key })
	for _, d := range s.devices {
		devices = append(devices, d...)
	}
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].Kind != devices[j].Kind {
			return devices[i].Kind < devices[j].Kind
		}
		return devices[i].ID < devices[j].ID
	})
	for _, st := range s.status {
		providers = append(providers, st)
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })
	return series, devices, providers
}

// Snapshot returns the latest value of every series and the keys that are stale.
func (s *Store) Snapshot() (t int64, values map[string]float64, stale []string) {
	now := s.now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	values = make(map[string]float64, len(s.series))
	for k, sr := range s.series {
		values[k] = sr.last
		if now.Sub(sr.at) > StaleAfter {
			stale = append(stale, k)
		}
	}
	sort.Strings(stale)
	return now.UnixMilli(), values, stale
}

// Points is one series in column form.
type Points struct {
	T []int64   `json:"t"`
	V []float64 `json:"v"`
}

// History returns the ring contents of the given keys over the last d, averaged into buckets of step (0 = raw).
// Unknown keys are omitted.
func (s *Store) History(keys []string, d, step time.Duration) map[string]Points {
	to := s.now().UnixMilli()
	from := to - d.Milliseconds()
	out := make(map[string]Points, len(keys))
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range keys {
		sr, ok := s.series[k]
		if !ok {
			continue
		}
		p := Points{T: []int64{}, V: []float64{}}
		if step <= 0 {
			sr.ring.each(from, to, func(t int64, v float64) { p.T, p.V = append(p.T, t), append(p.V, v) })
		} else {
			bucket, sum, n := int64(-1), 0.0, 0
			flush := func() {
				if n > 0 {
					p.T, p.V = append(p.T, bucket*step.Milliseconds()), append(p.V, sum/float64(n))
				}
			}
			sr.ring.each(from, to, func(t int64, v float64) {
				b := t / step.Milliseconds()
				if b != bucket {
					flush()
					bucket, sum, n = b, 0, 0
				}
				sum += v
				n++
			})
			flush()
		}
		out[k] = p
	}
	return out
}

// Keys returns every series key matching one of the patterns (exact, or "prefix.*"); no patterns = all.
func (s *Store) Keys(patterns []string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	for k := range s.series {
		if Match(patterns, k) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Match reports whether key is selected by patterns (none selects everything).
func Match(patterns []string, key string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if p == key || (strings.HasSuffix(p, ".*") && strings.HasPrefix(key, p[:len(p)-1])) || p == "*" {
			return true
		}
	}
	return false
}

// ---- live frames -------------------------------------------------------------------------------------------------

// Subscription delivers frames; a slow consumer loses frames (counted in Dropped) instead of blocking collection.
type Subscription struct {
	C       chan Frame
	mu      sync.Mutex
	dropped int
	store   *Store
}

// Subscribe registers a consumer.
func (s *Store) Subscribe() *Subscription {
	sub := &Subscription{C: make(chan Frame, subscriberQ), store: s}
	s.subMu.Lock()
	s.subs[sub] = struct{}{}
	s.subMu.Unlock()
	return sub
}

// Dropped returns and resets the number of frames lost since the last call.
func (sub *Subscription) Dropped() int {
	sub.mu.Lock()
	defer sub.mu.Unlock()
	n := sub.dropped
	sub.dropped = 0
	return n
}

// Close unregisters the subscription.
func (sub *Subscription) Close() {
	sub.store.subMu.Lock()
	delete(sub.store.subs, sub)
	sub.store.subMu.Unlock()
}

func (s *Store) publish(f Frame) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for sub := range s.subs {
		select {
		case sub.C <- f:
		default:
			sub.mu.Lock()
			sub.dropped++
			sub.mu.Unlock()
		}
	}
}
