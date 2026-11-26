package metrics

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// nvidiaQuery is the exact field list the parser expects (docs/modules/dashboard.md §3).
const nvidiaQuery = "index,name,utilization.gpu,temperature.gpu,memory.used,memory.total,power.draw,power.limit,fan.speed"

type gpuReading struct {
	idx                       int
	name                      string
	util, temp, memUsed, memT float64 // memory in MiB
	power, limit, fan         float64
	at                        time.Time
}

func num(s string) float64 {
	s = strings.TrimSpace(s)
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v
	}
	return math.NaN() // "[N/A]", "[Not Supported]", ""
}

// parseNvidiaLine parses one `--format=csv,noheader,nounits` line of nvidiaQuery.
func parseNvidiaLine(line string) (gpuReading, error) {
	f := strings.Split(line, ",")
	if len(f) != 9 {
		return gpuReading{}, fmt.Errorf("unexpected nvidia-smi line with %d fields", len(f))
	}
	idx, err := strconv.Atoi(strings.TrimSpace(f[0]))
	if err != nil {
		return gpuReading{}, err
	}
	return gpuReading{idx: idx, name: strings.TrimSpace(f[1]), util: num(f[2]), temp: num(f[3]), memUsed: num(f[4]), memT: num(f[5]),
		power: num(f[6]), limit: num(f[7]), fan: num(f[8])}, nil
}

// NVIDIA runs one long-lived `nvidia-smi -l 1` and serves its latest readings. The process is restarted with
// back-off if it dies; Sample fails (card shows the reason) while no fresh data arrives.
type NVIDIA struct {
	bin   string
	start func(ctx context.Context) (io.ReadCloser, func() error, error) // injectable for tests
	now   func() time.Time
	mu    sync.Mutex
	last  map[int]gpuReading
	err   string
}

// NewNVIDIA returns nil when nvidia-smi is not installed (no GPU widget at all).
func NewNVIDIA() *NVIDIA {
	bin, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return nil
	}
	g := &NVIDIA{bin: bin, now: time.Now, last: map[int]gpuReading{}}
	g.start = func(ctx context.Context) (io.ReadCloser, func() error, error) {
		cmd := exec.CommandContext(ctx, g.bin, "--query-gpu="+nvidiaQuery, "--format=csv,noheader,nounits", "-l", "1")
		out, err := cmd.StdoutPipe()
		if err != nil {
			return nil, nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, nil, err
		}
		return out, cmd.Wait, nil
	}
	return g
}

func (g *NVIDIA) Name() string            { return "gpu" }
func (g *NVIDIA) Interval() time.Duration { return time.Second }

// Start launches the reader goroutine.
func (g *NVIDIA) Start(ctx context.Context) { go g.run(ctx) }

func (g *NVIDIA) run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		out, wait, err := g.start(ctx)
		if err == nil {
			sc := bufio.NewScanner(out)
			for sc.Scan() {
				if r, perr := parseNvidiaLine(sc.Text()); perr == nil {
					r.at = g.now()
					g.mu.Lock()
					g.last[r.idx], g.err = r, ""
					g.mu.Unlock()
					backoff = time.Second
				}
			}
			err = wait()
			if err == nil {
				err = errors.New("nvidia-smi exited")
			}
		}
		g.mu.Lock()
		g.err = "nvidia-smi: " + err.Error()
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (g *NVIDIA) readings() []gpuReading {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]gpuReading, 0, len(g.last))
	for _, r := range g.last {
		out = append(out, r)
	}
	return out
}

func (g *NVIDIA) Devices(context.Context) []Device {
	var out []Device
	for _, r := range g.readings() {
		out = append(out, Device{Kind: "gpu", ID: strconv.Itoa(r.idx), Label: r.name, Props: map[string]any{"vendor": "nvidia"}})
	}
	sortDevices(out)
	return out
}

func (g *NVIDIA) Sample(_ context.Context, _ time.Time) ([]Sample, error) {
	g.mu.Lock()
	msg := g.err
	g.mu.Unlock()
	rs := g.readings()
	fresh := rs[:0]
	for _, r := range rs {
		if g.now().Sub(r.at) <= StaleAfter {
			fresh = append(fresh, r)
		}
	}
	if len(fresh) == 0 {
		if msg == "" {
			msg = "waiting for nvidia-smi"
		}
		return nil, errors.New(msg)
	}
	var out []Sample
	for _, r := range fresh {
		id := strconv.Itoa(r.idx)
		add := func(k, label, unit string, v float64) {
			if !math.IsNaN(v) {
				out = append(out, Sample{Meta{"gpu." + id + "." + k, label, unit, "gpu", id}, v})
			}
		}
		add("util", "Utilisation", "percent", r.util)
		add("temp", "Temperature", "celsius", r.temp)
		add("vram.used", "VRAM used", "bytes", r.memUsed*1024*1024)
		add("vram.total", "VRAM total", "bytes", r.memT*1024*1024)
		add("power", "Power", "watt", r.power)
		add("power.limit", "Power limit", "watt", r.limit)
		add("fan", "Fan", "percent", r.fan)
	}
	return out, nil
}
