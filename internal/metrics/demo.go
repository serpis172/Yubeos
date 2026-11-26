package metrics

import (
	"fmt"
	"math"
	"time"
)

// Demo produces smooth, deterministic mock data (same seed + same time = same values) so the dashboard can be
// developed without a server (design goal 6, --demo).
type Demo struct{ seed int64 }

func NewDemo(seed int64) *Demo { return &Demo{seed: seed} }

func (*Demo) Name() string { return "demo" }

const gib = 1 << 30

func (*Demo) Devices() []Device {
	return []Device{
		{Kind: "cpu", ID: "0", Label: "Demo CPU 4-core", Props: map[string]any{"cores": 2, "threads": 4, "logical": []int{0, 1, 2, 3}}},
		{Kind: "net", ID: "eth0", Label: "eth0", Props: map[string]any{"up": true, "speedBitsPerSec": 10e9}},
		{Kind: "net", ID: "docker0", Label: "docker0", Hidden: true, Props: map[string]any{"up": true}},
		{Kind: "gpu", ID: "0", Label: "Demo GPU", Props: map[string]any{}},
		{Kind: "disk", ID: "sda", Label: "Demo HDD 14TB", Props: map[string]any{"type": "hdd", "capacityBytes": 14e12}},
		{Kind: "disk", ID: "nvme0n1", Label: "Demo NVMe 1TB", Props: map[string]any{"type": "nvme", "capacityBytes": 1e12}},
	}
}

// wave is a smooth value in [0,1] that differs per (seed, salt, period).
func (d *Demo) wave(t time.Time, salt float64, period float64) float64 {
	x := float64(t.Unix())/period + salt + float64(d.seed%97)
	return (math.Sin(x)+math.Sin(x*0.37+1.3))/4 + 0.5
}

func (d *Demo) Sample(now time.Time) (map[string]float64, error) {
	v := map[string]float64{}
	var sum float64
	for i := 0; i < 4; i++ {
		u := 100 * d.wave(now, float64(i)*1.7, 17)
		v[fmt.Sprintf("cpu.core.%d.util", i)] = u
		sum += u
	}
	v["cpu.0.util"] = sum / 4
	v["cpu.0.temp"] = 40 + 25*d.wave(now, 9, 31)
	v["cpu.0.freq"] = (1.8 + 1.4*d.wave(now, 4, 23)) * 1e9

	used := (8 + 6*d.wave(now, 2, 120)) * gib
	v["mem.total"], v["mem.used"], v["mem.cache"], v["mem.arc"] = 32*gib, used, 6*gib, 10*gib
	v["mem.free"] = 32*gib - used - 16*gib
	v["mem.swap.used"] = 0.2 * gib

	v["net.eth0.rx"] = 120e6 * d.wave(now, 5, 13)
	v["net.eth0.tx"] = 20e6 * d.wave(now, 6, 19)
	v["net.eth0.errors"], v["net.eth0.speed"] = 0, 10e9
	v["net.docker0.rx"], v["net.docker0.tx"], v["net.docker0.errors"] = 1e5, 1e5, 0

	v["gpu.0.util"] = 100 * d.wave(now, 7, 11)
	v["gpu.0.temp"] = 45 + 25*d.wave(now, 8, 29)
	v["gpu.0.vram.used"], v["gpu.0.vram.total"] = 6*gib, 24*gib
	v["gpu.0.power"], v["gpu.0.power.limit"], v["gpu.0.fan"] = 80+150*d.wave(now, 3, 15), 350, 30+30*d.wave(now, 3, 15)

	for i, id := range []string{"sda", "nvme0n1"} {
		w := d.wave(now, float64(10+i), float64(14+i*5))
		v["disk."+id+".read"] = (90e6 - float64(i)*40e6) * w
		v["disk."+id+".write"] = 20e6 * w
		v["disk."+id+".iops"] = 400 * w
		v["disk."+id+".util"] = 100 * w
		v["disk."+id+".temp"] = 34 + 8*w
	}

	v["sys.load1"] = 0.5 + 3*d.wave(now, 1, 60)
	v["sys.load5"] = 0.5 + 2.5*d.wave(now, 1, 200)
	v["sys.load15"] = 0.5 + 2*d.wave(now, 1, 600)
	v["sys.uptime"] = float64(now.Unix() % (30 * 86400))
	return v, nil
}
