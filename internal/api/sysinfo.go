package api

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/serpis172/Yubeos/internal/version"
)

// SystemInfo mirrors components.schemas.SystemInfo in api/openapi.yaml.
type SystemInfo struct {
	Hostname      string            `json:"hostname"`
	Version       string            `json:"version"`
	DemoMode      bool              `json:"demoMode"`
	UptimeSeconds int64             `json:"uptimeSeconds"`
	OS            OSInfo            `json:"os"`
	Versions      map[string]string `json:"versions"`
	Capabilities  map[string]bool   `json:"capabilities"`
}

type OSInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Kernel  string `json:"kernel"`
}

// capabilityBinaries maps a capability to the executable whose presence enables it.
// Version probing (docs/ARCHITECTURE.md §14) is added with the modules that need it.
var capabilityBinaries = map[string]string{
	"zfs":        "zpool",
	"smart":      "smartctl",
	"samba":      "smbd",
	"nfs":        "exportfs",
	"docker":     "docker",
	"nvidia":     "nvidia-smi",
	"systemdRun": "systemd-run",
}

// hostFacts are the slow-changing parts of SystemInfo.
type hostFacts struct {
	Hostname     string
	OS           OSInfo
	Capabilities map[string]bool
}

func readHostFacts(demo bool) hostFacts {
	if demo {
		caps := make(map[string]bool, len(capabilityBinaries))
		for k := range capabilityBinaries {
			caps[k] = true // every provider has a mock
		}
		return hostFacts{Hostname: "yubeos-demo", OS: OSInfo{Name: "Ubuntu", Version: "24.04 LTS (demo)", Kernel: "6.8.0-demo"}, Capabilities: caps}
	}
	host, _ := os.Hostname()
	rel := parseOSRelease("/etc/os-release")
	kernel, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	caps := make(map[string]bool, len(capabilityBinaries))
	for k, bin := range capabilityBinaries {
		_, err := exec.LookPath(bin)
		caps[k] = err == nil
	}
	return hostFacts{Hostname: host, OS: OSInfo{Name: rel["NAME"], Version: rel["VERSION"], Kernel: strings.TrimSpace(string(kernel))}, Capabilities: caps}
}

func buildSystemInfo(demo bool, f hostFacts, now, start time.Time) SystemInfo {
	up := hostUptime()
	if demo {
		up = int64(now.Sub(start).Seconds()) + 86400*3
	}
	return SystemInfo{
		Hostname: f.Hostname, Version: version.Version, DemoMode: demo, UptimeSeconds: up,
		OS: f.OS, Versions: map[string]string{}, Capabilities: f.Capabilities,
	}
}

// parseOSRelease reads KEY=VALUE pairs (values optionally quoted) from an os-release file.
func parseOSRelease(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if uq, err := strconv.Unquote(v); err == nil {
			v = uq
		} else {
			v = strings.Trim(v, `'"`)
		}
		out[k] = v
	}
	return out
}

func hostUptime() int64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	f, _, _ := strings.Cut(strings.TrimSpace(string(b)), " ")
	secs, err := strconv.ParseFloat(f, 64)
	if err != nil {
		return 0
	}
	return int64(secs)
}
