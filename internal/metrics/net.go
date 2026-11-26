package metrics

import (
	"context"
	"strconv"
	"strings"
	"time"
)

type netCounters struct{ rx, tx, errs, drops uint64 }

// parseNetDev parses /proc/net/dev.
func parseNetDev(s string) map[string]netCounters {
	out := map[string]netCounters{}
	for _, line := range strings.Split(s, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 16 {
			continue
		}
		u := func(i int) uint64 { v, _ := strconv.ParseUint(f[i], 10, 64); return v }
		// rx: bytes packets errs drop fifo frame compressed multicast | tx: bytes packets errs drop ...
		out[strings.TrimSpace(name)] = netCounters{rx: u(0), tx: u(8), errs: u(2) + u(10), drops: u(3) + u(11)}
	}
	return out
}

// isVirtualIface marks interfaces hidden by default on the dashboard.
func isVirtualIface(n string) bool {
	if n == "lo" {
		return true
	}
	for _, p := range []string{"veth", "docker", "br-", "virbr", "tun", "tap", "cali", "flannel", "cni"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// Network reports throughput, link speed and errors per interface.
type Network struct {
	fs     FS
	prev   map[string]netCounters
	prevAt time.Time
}

func NewNetwork(fs FS) *Network { return &Network{fs: fs} }

func (n *Network) Name() string            { return "net" }
func (n *Network) Interval() time.Duration { return time.Second }

func (n *Network) Devices(context.Context) []Device {
	s, err := n.fs.Read("/proc/net/dev")
	if err != nil {
		return nil
	}
	var out []Device
	for name := range parseNetDev(s) {
		props := map[string]any{"up": n.fs.ReadTrim("/sys/class/net/"+name+"/operstate") == "up"}
		if sp, ok := n.fs.ReadInt("/sys/class/net/" + name + "/speed"); ok && sp > 0 {
			props["speedBitsPerSec"] = sp * 1_000_000
		}
		out = append(out, Device{Kind: "net", ID: name, Label: name, Hidden: isVirtualIface(name), Props: props})
	}
	sortDevices(out)
	return out
}

func (n *Network) Sample(_ context.Context, now time.Time) ([]Sample, error) {
	s, err := n.fs.Read("/proc/net/dev")
	if err != nil {
		return nil, err
	}
	cur := parseNetDev(s)
	prev, prevAt := n.prev, n.prevAt
	n.prev, n.prevAt = cur, now
	if prev == nil {
		return nil, nil
	}
	dt := now.Sub(prevAt).Seconds()
	if dt <= 0 {
		return nil, nil
	}
	var out []Sample
	for name, c := range cur {
		p, ok := prev[name]
		if !ok {
			continue
		}
		m := func(k, label, unit string) Meta { return Meta{"net." + name + "." + k, label, unit, "net", name} }
		// A counter that went backwards means a reset (interface re-created, 32-bit wrap): report 0 for this round.
		rate := func(cur, prev uint64) float64 {
			if cur < prev {
				return 0
			}
			return float64(cur-prev) / dt
		}
		out = append(out,
			Sample{m("rx", "Download", "bytes/s"), rate(c.rx, p.rx)},
			Sample{m("tx", "Upload", "bytes/s"), rate(c.tx, p.tx)},
			Sample{m("errors", "Errors and drops", "count"), float64(c.errs + c.drops)},
		)
		if sp, ok := n.fs.ReadInt("/sys/class/net/" + name + "/speed"); ok && sp > 0 {
			out = append(out, Sample{m("speed", "Link speed", "bit/s"), float64(sp) * 1e6})
		}
	}
	return out, nil
}
