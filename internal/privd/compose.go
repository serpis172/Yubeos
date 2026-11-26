package privd

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/serpis172/Yubeos/internal/priv"
)

// InstalledAddon is one Yubeos-managed compose app found on the host.
type InstalledAddon struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Version       string   `json:"version,omitempty"`
	Description   string   `json:"description,omitempty"`
	Icon          string   `json:"icon,omitempty"`
	Categories    []string `json:"categories,omitempty"`
	State         string   `json:"state"`
	Embed         string   `json:"embed"`
	Path          string   `json:"path"`
	Sidebar       bool     `json:"sidebar"`
	HostPort      int      `json:"hostPort,omitempty"`
	ContainerPort int      `json:"containerPort,omitempty"`
}

var (
	containerID = regexp.MustCompile(`^[0-9a-f]{12,64}$`)
	addonID     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`) // becomes a DNS label: strict on purpose
)

// RegisterCompose adds the read-only compose.list method (docs/modules/addons.md). It only inspects containers
// that carry Yubeos' own label; it never starts, stops or changes anything.
func RegisterCompose(s *Server) {
	s.Register(Method{Name: "compose.list", Class: Read, Handle: func(ctx context.Context, _ priv.Meta, _ json.RawMessage) (any, error) {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		ids, err := exec.CommandContext(cctx, "docker", "ps", "-aq", "--no-trunc", "--filter", "label=dev.yubeos.managed=true").Output()
		if err != nil {
			return nil, priv.Errorf(priv.CodeUnavailable, "docker not available: %v", err)
		}
		args := []string{"inspect"}
		for _, id := range strings.Fields(string(ids)) {
			if containerID.MatchString(id) {
				args = append(args, id)
			}
		}
		if len(args) == 1 {
			return map[string]any{"items": []InstalledAddon{}}, nil
		}
		raw, err := exec.CommandContext(cctx, "docker", args...).Output()
		if err != nil {
			return nil, priv.Errorf(priv.CodeUnavailable, "docker inspect failed: %v", err)
		}
		items, err := ParseInspect(raw)
		if err != nil {
			return nil, priv.Errorf(priv.CodeInternal, "unexpected docker output: %v", err)
		}
		return map[string]any{"items": items}, nil
	}})
}

type inspected struct {
	State           struct{ Status string }
	Config          struct{ Labels map[string]string }
	NetworkSettings struct {
		Ports map[string][]struct{ HostPort string }
	}
}

// ParseInspect turns `docker inspect` JSON into add-ons: one per dev.yubeos.addon id, containers merged.
func ParseInspect(raw []byte) ([]InstalledAddon, error) {
	var cs []inspected
	if err := json.Unmarshal(raw, &cs); err != nil {
		return nil, err
	}
	items := []InstalledAddon{}
	at := map[string]int{}
	for _, c := range cs {
		l := c.Config.Labels
		id := l["dev.yubeos.addon"]
		if !addonID.MatchString(id) {
			continue
		}
		state := mapState(c.State.Status)
		i, seen := at[id]
		if !seen {
			a := InstalledAddon{
				ID: id, Name: orDefault(l["dev.yubeos.name"], id), Version: l["dev.yubeos.version"],
				Description: l["dev.yubeos.description"], Icon: l["dev.yubeos.icon"], State: state,
				Embed: orDefault(l["dev.yubeos.ui.embed"], "link"), Path: orDefault(l["dev.yubeos.ui.path"], "/"),
				Sidebar: l["dev.yubeos.ui.sidebar"] != "false",
			}
			if a.Embed != "iframe" && a.Embed != "link" && a.Embed != "none" {
				a.Embed = "link"
			}
			if cat := l["dev.yubeos.categories"]; cat != "" {
				a.Categories = strings.Split(cat, ",")
			}
			items = append(items, a)
			i = len(items) - 1
			at[id] = i
		} else if state == "running" {
			items[i].State = "running" // one running container is enough to call the app running
		}
		if p, err := strconv.Atoi(l["dev.yubeos.ui.port"]); err == nil && items[i].HostPort == 0 {
			items[i].ContainerPort = p
			for _, b := range c.NetworkSettings.Ports[strconv.Itoa(p)+"/tcp"] {
				if hp, err := strconv.Atoi(b.HostPort); err == nil && hp > 0 {
					items[i].HostPort = hp
					break
				}
			}
		}
	}
	return items, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func mapState(s string) string {
	switch strings.ToLower(s) {
	case "running":
		return "running"
	case "restarting", "paused":
		return "starting"
	case "exited", "dead", "created":
		return "stopped"
	default:
		return "unknown"
	}
}
