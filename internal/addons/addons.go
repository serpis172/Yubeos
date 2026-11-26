// Package addons lists the add-ons installed on the host and works out a safe URL to open each in a new tab
// (ADR-0018). Install/update/rollback and the ticketed proxy are Phase 7; this is the read-only part.
package addons

import (
	"context"
	"net"
	"strconv"
	"strings"

	"github.com/serpis172/Yubeos/internal/priv"
)

// Addon is one installed add-on as found on the host (no URL yet).
type Addon struct {
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

// Service lists installed add-ons.
type Service interface {
	List(ctx context.Context) ([]Addon, error)
}

// Priv discovers add-ons through the root helper's read-only compose.list method.
type Priv struct{ Caller priv.Caller }

func (p Priv) List(ctx context.Context) ([]Addon, error) {
	var out struct {
		Items []Addon `json:"items"`
	}
	if err := p.Caller.Call(ctx, "compose.list", priv.Meta{}, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// Demo returns fixed mock add-ons so the UI can be built without a server (design goal 6).
type Demo struct{}

func (Demo) List(context.Context) ([]Addon, error) {
	return []Addon{
		{ID: "jellyfin", Name: "Jellyfin", Version: "10.10.3-1", Description: "Media server", Categories: []string{"media"}, State: "running", Embed: "link", Path: "/", Sidebar: true, HostPort: 20001, ContainerPort: 8096},
		{ID: "filebrowser", Name: "File Browser", Version: "2.31.2-1", Description: "Web file manager", Categories: []string{"files"}, State: "running", Embed: "link", Path: "/", Sidebar: true, HostPort: 20002, ContainerPort: 80},
		{ID: "uptime-kuma", Name: "Uptime Kuma", Version: "1.23.15-1", Description: "Monitoring", Categories: []string{"monitoring"}, State: "stopped", Embed: "link", Path: "/", Sidebar: true, HostPort: 20003, ContainerPort: 3001},
	}, nil
}

// UIURL computes the URL that opens an add-on in a browser tab, per ADR-0018.
// requestHost is the host the browser used to reach Yubeos; lanHost is the server's own LAN address.
// It returns (url, blocked). A non-empty blocked says why no safe link exists; embed "none" gives neither.
func UIURL(requestHost, baseDomain, lanHost string, a Addon) (url, blocked string) {
	if a.Embed == "none" {
		return "", ""
	}
	path := safePath(a.Path)
	if baseDomain != "" { // isolated subdomain (preferred): never Yubeos' own origin
		return "https://" + a.ID + "." + baseDomain + path, ""
	}
	if a.HostPort == 0 {
		return "", "no-port"
	}
	if lanHost == "" {
		return "", "no-alternate-address"
	}
	if strings.EqualFold(hostOnly(lanHost), hostOnly(requestHost)) {
		return "", "same-host" // cookies are not isolated by port: refuse (ADR-0018)
	}
	return "http://" + net.JoinHostPort(lanHost, strconv.Itoa(a.HostPort)) + path, ""
}

// safePath keeps a label-supplied path from turning into another host ("//evil") or a scheme.
func safePath(p string) string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.ContainsAny(p, "\\\r\n\t ") {
		return "/"
	}
	return p
}

func hostOnly(h string) string {
	if hh, _, err := net.SplitHostPort(h); err == nil {
		h = hh
	}
	return strings.Trim(strings.ToLower(h), "[]")
}
