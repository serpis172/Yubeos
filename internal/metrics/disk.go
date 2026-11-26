package metrics

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"
)

type diskCounters struct{ reads, writes, readSectors, writeSectors, ioMs uint64 }

// parseDiskstats parses /proc/diskstats.
func parseDiskstats(s string) map[string]diskCounters {
	out := map[string]diskCounters{}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 13 {
			continue
		}
		u := func(i int) uint64 { v, _ := strconv.ParseUint(f[i], 10, 64); return v }
		// major minor name reads merged sectors ms | writes merged sectors ms | inflight io_ms weighted_ms
		out[f[2]] = diskCounters{reads: u(3), readSectors: u(5), writes: u(7), writeSectors: u(9), ioMs: u(12)}
	}
	return out
}

func skipBlock(n string) bool {
	for _, p := range []string{"loop", "ram", "zram", "dm-", "md", "zd", "sr", "fd", "nbd"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// Disks reports throughput, IOPS, utilisation and temperature per whole block device.
type Disks struct {
	fs     FS
	prev   map[string]diskCounters
	prevAt time.Time
}

func NewDisks(fs FS) *Disks { return &Disks{fs: fs} }

func (d *Disks) Name() string            { return "disk" }
func (d *Disks) Interval() time.Duration { return 2 * time.Second }

func (d *Disks) blocks() []string {
	var out []string
	for _, n := range d.fs.Dir("/sys/block") {
		if !skipBlock(n) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func (d *Disks) Devices(context.Context) []Device {
	var out []Device
	for _, n := range d.blocks() {
		base := "/sys/block/" + n + "/"
		model := d.fs.ReadTrim(base + "device/model")
		kind := "hdd"
		switch {
		case strings.HasPrefix(n, "nvme"):
			kind = "nvme"
		case d.fs.ReadTrim(base+"queue/rotational") == "0":
			kind = "ssd"
		}
		props := map[string]any{"type": kind}
		if sec, ok := d.fs.ReadInt(base + "size"); ok {
			props["capacityBytes"] = sec * 512
		}
		label := model
		if label == "" {
			label = n
		}
		out = append(out, Device{Kind: "disk", ID: n, Label: label, Props: props})
	}
	return out
}

// temps maps block name → °C from hwmon drivetemp (SATA/SAS) and nvme.
func (d *Disks) temps() map[string]float64 {
	out := map[string]float64{}
	for _, h := range d.fs.Dir("/sys/class/hwmon") {
		base := "/sys/class/hwmon/" + h + "/"
		name := d.fs.ReadTrim(base + "name")
		if name != "drivetemp" && name != "nvme" {
			continue
		}
		v, ok := d.fs.ReadInt(base + "temp1_input")
		if !ok {
			continue
		}
		var blocks []string
		blocks = append(blocks, d.fs.Dir(base+"device/block")...)
		for _, e := range d.fs.Dir(base + "device") {
			if strings.HasPrefix(e, "nvme") && strings.Contains(e, "n") && e != "nvme" {
				blocks = append(blocks, e)
			}
		}
		for _, b := range blocks {
			out[b] = float64(v) / 1000
		}
	}
	return out
}

func (d *Disks) Sample(_ context.Context, now time.Time) ([]Sample, error) {
	s, err := d.fs.Read("/proc/diskstats")
	if err != nil {
		return nil, err
	}
	cur := parseDiskstats(s)
	prev, prevAt := d.prev, d.prevAt
	d.prev, d.prevAt = cur, now
	if prev == nil {
		return nil, nil
	}
	dt := now.Sub(prevAt).Seconds()
	if dt <= 0 {
		return nil, nil
	}
	temps := d.temps()
	delta := func(a, b uint64) float64 {
		if a < b {
			return 0
		}
		return float64(a - b)
	}
	var out []Sample
	for _, n := range d.blocks() {
		c, ok := cur[n]
		p, ok2 := prev[n]
		if !ok || !ok2 {
			continue
		}
		m := func(k, label, unit string) Meta { return Meta{"disk." + n + "." + k, label, unit, "disk", n} }
		util := delta(c.ioMs, p.ioMs) / (dt * 1000) * 100
		if util > 100 {
			util = 100
		}
		out = append(out,
			Sample{m("read", "Read", "bytes/s"), delta(c.readSectors, p.readSectors) * 512 / dt},
			Sample{m("write", "Write", "bytes/s"), delta(c.writeSectors, p.writeSectors) * 512 / dt},
			Sample{m("iops", "IOPS", "count/s"), (delta(c.reads, p.reads) + delta(c.writes, p.writes)) / dt},
			Sample{m("util", "Busy", "percent"), util},
		)
		if t, ok := temps[n]; ok {
			out = append(out, Sample{m("temp", "Temperature", "celsius"), t})
		}
	}
	return out, nil
}

func sortDevices(d []Device) {
	sort.Slice(d, func(i, j int) bool { return d[i].ID < d[j].ID })
}
