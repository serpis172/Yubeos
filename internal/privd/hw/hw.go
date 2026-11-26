// Package hw holds read-only hardware methods of the root helper: sources the unprivileged daemon cannot read.
package hw

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/serpis172/Yubeos/internal/priv"
	"github.com/serpis172/Yubeos/internal/privd"
)

// Zone is one top-level RAPL (powercap) zone.
type Zone struct {
	Name     string `json:"name"`
	EnergyUJ uint64 `json:"energyUj"`
	MaxUJ    uint64 `json:"maxEnergyRangeUj"`
}

func readUint(p string) (uint64, bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return v, err == nil
}

// ReadRAPL lists the top-level zones (intel-rapl:N, also used by AMD) below sysRoot ("/" in production).
func ReadRAPL(sysRoot string) []Zone {
	base := filepath.Join(sysRoot, "sys/class/powercap")
	es, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []Zone
	for _, e := range es {
		n := e.Name()
		// top-level zones look like "intel-rapl:0"; sub-zones ("intel-rapl:0:0") are cores/uncore/dram
		if !strings.HasPrefix(n, "intel-rapl:") || strings.Count(n, ":") != 1 {
			continue
		}
		name, err := os.ReadFile(filepath.Join(base, n, "name"))
		if err != nil {
			continue
		}
		e, ok := readUint(filepath.Join(base, n, "energy_uj"))
		if !ok {
			continue
		}
		max, _ := readUint(filepath.Join(base, n, "max_energy_range_uj"))
		out = append(out, Zone{Name: strings.TrimSpace(string(name)), EnergyUJ: e, MaxUJ: max})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Register adds the hardware methods.
func Register(srv *privd.Server, sysRoot string) {
	srv.Register(privd.Method{Name: "rapl.read", Class: privd.Read, Handle: func(_ context.Context, _ priv.Meta, _ json.RawMessage) (any, error) {
		return struct {
			Zones []Zone `json:"zones"`
		}{ReadRAPL(sysRoot)}, nil
	}})
}
