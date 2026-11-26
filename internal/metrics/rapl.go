package metrics

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/serpis172/Yubeos/internal/priv"
)

// RAPLZone is one package-level energy counter returned by the helper method rapl.read.
type RAPLZone struct {
	Name     string `json:"name"` // package-0, package-1, ...
	EnergyUJ uint64 `json:"energyUj"`
	MaxUJ    uint64 `json:"maxEnergyRangeUj"`
}

// RAPL reports CPU package power. The energy counters are root-only since 2020 (a side channel), so they are read
// through the privileged helper; without it the power values are simply absent.
type RAPL struct {
	call   priv.Caller
	prev   map[string]uint64
	prevAt time.Time
}

func NewRAPL(c priv.Caller) *RAPL { return &RAPL{call: c} }

func (r *RAPL) Name() string                     { return "rapl" }
func (r *RAPL) Interval() time.Duration          { return time.Second }
func (r *RAPL) Devices(context.Context) []Device { return nil }

func (r *RAPL) Sample(ctx context.Context, now time.Time) ([]Sample, error) {
	var res struct {
		Zones []RAPLZone `json:"zones"`
	}
	if err := r.call.Call(ctx, "rapl.read", priv.Meta{Actor: "metrics"}, nil, &res); err != nil {
		return nil, errors.New("RAPL requires the privileged helper: " + err.Error())
	}
	cur := map[string]uint64{}
	for _, z := range res.Zones {
		cur[z.Name] = z.EnergyUJ
	}
	prev, prevAt := r.prev, r.prevAt
	r.prev, r.prevAt = cur, now
	if prev == nil {
		return nil, nil
	}
	dt := now.Sub(prevAt).Seconds()
	if dt <= 0 {
		return nil, nil
	}
	var out []Sample
	for _, z := range res.Zones {
		p, ok := prev[z.Name]
		if !ok || !strings.HasPrefix(z.Name, "package-") {
			continue
		}
		d := z.EnergyUJ - p
		if z.EnergyUJ < p { // counter wrapped at max_energy_range_uj
			if z.MaxUJ == 0 {
				continue
			}
			d = z.MaxUJ - p + z.EnergyUJ
		}
		id := strings.TrimPrefix(z.Name, "package-")
		if _, err := strconv.Atoi(id); err != nil {
			continue
		}
		out = append(out, Sample{Meta{"cpu." + id + ".power", "Package power", "watt", "cpu", id}, float64(d) / 1e6 / dt})
	}
	return out, nil
}
