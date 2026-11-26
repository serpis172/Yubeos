package metrics

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Proc reads /proc and /sys as an unprivileged user (no root helper needed). Temperatures, GPU, power and
// SMART data need extra providers and are not collected here.
type Proc struct {
	procRoot, sysRoot string

	mu   sync.Mutex
	prev *procPrev
	devs []Device
}

type procPrev struct {
	at    time.Time
	cpu   map[string][2]float64 // name -> {idle, total}
	net   map[string][4]float64 // iface -> {rxBytes, txBytes, rxErr, txErr}
	disks map[string][4]float64 // name -> {sectorsRead, sectorsWritten, ios, msBusy}
}

// NewProc reads the real /proc and /sys.
func NewProc() *Proc { return &Proc{procRoot: "/proc", sysRoot: "/sys"} }

func (p *Proc) Name() string { return "procfs" }

func (p *Proc) Devices() []Device {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Device(nil), p.devs...)
}

var (
	diskName    = regexp.MustCompile(`^(sd[a-z]+|vd[a-z]+|xvd[a-z]+|hd[a-z]+|nvme[0-9]+n[0-9]+|mmcblk[0-9]+)$`)
	virtualNet  = regexp.MustCompile(`^(docker|veth|br-|virbr|vnet|tun|tap|wg|tailscale|zt|cni|flannel|cali)`)
	errNoSource = errors.New("no readable /proc")
)

func (p *Proc) read(rel string) (string, error) {
	b, err := os.ReadFile(filepath.Join(p.procRoot, rel))
	return string(b), err
}

func (p *Proc) readSys(rel string) string {
	b, _ := os.ReadFile(filepath.Join(p.sysRoot, rel))
	return strings.TrimSpace(string(b))
}

// Sample implements Source. The first call only primes the counters, so rate series appear from the second.
func (p *Proc) Sample(now time.Time) (map[string]float64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]float64{}
	cur := &procPrev{at: now, cpu: map[string][2]float64{}, net: map[string][4]float64{}, disks: map[string][4]float64{}}
	var devs []Device
	ok := false

	// --- CPU utilisation (per thread and total)
	if txt, err := p.read("stat"); err == nil {
		ok = true
		for _, line := range strings.Split(txt, "\n") {
			f := strings.Fields(line)
			if len(f) < 5 || !strings.HasPrefix(f[0], "cpu") {
				continue
			}
			var total float64
			vals := make([]float64, 0, len(f)-1)
			for _, x := range f[1:] {
				v, _ := strconv.ParseFloat(x, 64)
				vals = append(vals, v)
			}
			for i, v := range vals {
				if i < 8 { // user..steal; guest is already counted inside user
					total += v
				}
			}
			idle := vals[3]
			if len(vals) > 4 {
				idle += vals[4] // iowait
			}
			cur.cpu[f[0]] = [2]float64{idle, total}
			if p.prev != nil {
				if pv, ok := p.prev.cpu[f[0]]; ok && total > pv[1] {
					util := 100 * (1 - (idle-pv[0])/(total-pv[1]))
					key := "cpu.0.util"
					if f[0] != "cpu" {
						key = "cpu.core." + strings.TrimPrefix(f[0], "cpu") + ".util"
					}
					out[key] = clamp(util, 0, 100)
				}
			}
		}
		devs = append(devs, p.cpuDevice(cur.cpu))
		if hz := p.cpuFreq(); hz > 0 {
			out["cpu.0.freq"] = hz
		}
	}

	// --- memory
	if txt, err := p.read("meminfo"); err == nil {
		ok = true
		mi := map[string]float64{}
		for _, line := range strings.Split(txt, "\n") {
			k, v, found := strings.Cut(line, ":")
			if !found {
				continue
			}
			n, _ := strconv.ParseFloat(strings.Fields(v + " 0")[0], 64)
			mi[k] = n * 1024
		}
		total, free := mi["MemTotal"], mi["MemFree"]
		cache := mi["Buffers"] + mi["Cached"] + mi["SReclaimable"]
		used := total - free - cache
		if arc := p.zfsARC(); arc > 0 { // ZFS ARC is reclaimable cache but shows up as "used" on Linux
			out["mem.arc"] = arc
			used -= arc
		}
		out["mem.total"], out["mem.free"], out["mem.cache"], out["mem.used"] = total, free, cache, clamp(used, 0, total)
		if mi["SwapTotal"] > 0 {
			out["mem.swap.used"] = mi["SwapTotal"] - mi["SwapFree"]
		}
	}

	// --- load and uptime
	if txt, err := p.read("loadavg"); err == nil {
		ok = true
		f := strings.Fields(txt)
		for i, k := range []string{"sys.load1", "sys.load5", "sys.load15"} {
			if i < len(f) {
				out[k], _ = strconv.ParseFloat(f[i], 64)
			}
		}
	}
	if txt, err := p.read("uptime"); err == nil {
		if f := strings.Fields(txt); len(f) > 0 {
			out["sys.uptime"], _ = strconv.ParseFloat(f[0], 64)
		}
	}

	// --- network
	if txt, err := p.read("net/dev"); err == nil {
		ok = true
		dt := 0.0
		if p.prev != nil {
			dt = now.Sub(p.prev.at).Seconds()
		}
		for _, line := range strings.Split(txt, "\n") {
			name, rest, found := strings.Cut(line, ":")
			name = strings.TrimSpace(name)
			if !found || name == "" || name == "lo" {
				continue
			}
			f := strings.Fields(rest)
			if len(f) < 11 {
				continue
			}
			n := func(i int) float64 { v, _ := strconv.ParseFloat(f[i], 64); return v }
			c := [4]float64{n(0), n(8), n(2), n(10)}
			cur.net[name] = c
			out["net."+name+".errors"] = c[2] + c[3]
			if pv, ok := p.prev.netOf(name); ok && dt > 0 {
				out["net."+name+".rx"] = nonNeg(c[0]-pv[0]) / dt
				out["net."+name+".tx"] = nonNeg(c[1]-pv[1]) / dt
			}
			props := map[string]any{"up": p.readSys("class/net/"+name+"/operstate") != "down"}
			if mbit, err := strconv.ParseFloat(p.readSys("class/net/"+name+"/speed"), 64); err == nil && mbit > 0 {
				props["speedBitsPerSec"] = mbit * 1e6
			}
			devs = append(devs, Device{Kind: "net", ID: name, Label: name, Hidden: virtualNet.MatchString(name), Props: props})
		}
	}

	// --- block devices
	if txt, err := p.read("diskstats"); err == nil {
		ok = true
		dt := 0.0
		if p.prev != nil {
			dt = now.Sub(p.prev.at).Seconds()
		}
		for _, line := range strings.Split(txt, "\n") {
			f := strings.Fields(line)
			if len(f) < 14 || !diskName.MatchString(f[2]) {
				continue
			}
			name := f[2]
			n := func(i int) float64 { v, _ := strconv.ParseFloat(f[i], 64); return v }
			c := [4]float64{n(5), n(9), n(3) + n(7), n(12)}
			cur.disks[name] = c
			if pv, ok := p.prev.diskOf(name); ok && dt > 0 {
				out["disk."+name+".read"] = nonNeg(c[0]-pv[0]) * 512 / dt
				out["disk."+name+".write"] = nonNeg(c[1]-pv[1]) * 512 / dt
				out["disk."+name+".iops"] = nonNeg(c[2]-pv[2]) / dt
				out["disk."+name+".util"] = clamp(nonNeg(c[3]-pv[3])/(dt*10), 0, 100) // ms busy / (dt*1000) * 100
			}
			devs = append(devs, p.diskDevice(name))
		}
	}

	if !ok {
		return nil, errNoSource
	}
	sort.Slice(devs, func(i, j int) bool { return devs[i].Kind+devs[i].ID < devs[j].Kind+devs[j].ID })
	p.prev, p.devs = cur, devs
	return out, nil
}

func (pp *procPrev) netOf(n string) ([4]float64, bool) {
	if pp == nil {
		return [4]float64{}, false
	}
	v, ok := pp.net[n]
	return v, ok
}

func (pp *procPrev) diskOf(n string) ([4]float64, bool) {
	if pp == nil {
		return [4]float64{}, false
	}
	v, ok := pp.disks[n]
	return v, ok
}

func (p *Proc) cpuDevice(cpus map[string][2]float64) Device {
	threads := 0
	for k := range cpus {
		if k != "cpu" {
			threads++
		}
	}
	logical := make([]int, threads)
	for i := range logical {
		logical[i] = i
	}
	label, cores := "CPU", threads
	if txt, err := p.read("cpuinfo"); err == nil {
		for _, line := range strings.Split(txt, "\n") {
			k, v, found := strings.Cut(line, ":")
			if !found {
				continue
			}
			switch strings.TrimSpace(k) {
			case "model name":
				if label == "CPU" {
					label = strings.TrimSpace(v)
				}
			case "cpu cores":
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
					cores = n
				}
			}
		}
	}
	return Device{Kind: "cpu", ID: "0", Label: label, Props: map[string]any{"cores": cores, "threads": threads, "logical": logical}}
}

// cpuFreq returns the mean current frequency of all threads in Hz (0 if unknown).
func (p *Proc) cpuFreq() float64 {
	txt, err := p.read("cpuinfo")
	if err != nil {
		return 0
	}
	var sum float64
	var n int
	for _, line := range strings.Split(txt, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "cpu MHz" {
			if mhz, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				sum += mhz
				n++
			}
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n) * 1e6
}

func (p *Proc) zfsARC() float64 {
	txt, err := p.read("spl/kstat/zfs/arcstats")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(txt, "\n") {
		if f := strings.Fields(line); len(f) == 3 && f[0] == "size" {
			v, _ := strconv.ParseFloat(f[2], 64)
			return v
		}
	}
	return 0
}

func (p *Proc) diskDevice(name string) Device {
	label := p.readSys(fmt.Sprintf("block/%s/device/model", name))
	if label == "" {
		label = name
	}
	kind := "ssd"
	switch {
	case strings.HasPrefix(name, "nvme"):
		kind = "nvme"
	case p.readSys("block/"+name+"/queue/rotational") == "1":
		kind = "hdd"
	}
	props := map[string]any{"type": kind}
	if sectors, err := strconv.ParseFloat(p.readSys("block/"+name+"/size"), 64); err == nil {
		props["capacityBytes"] = sectors * 512
	}
	return Device{Kind: "disk", ID: name, Label: label, Props: props}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func nonNeg(v float64) float64 {
	if v < 0 {
		return 0 // counter reset or wrap
	}
	return v
}
