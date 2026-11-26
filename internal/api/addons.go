package api

import (
	"net"
	"net/http"

	"github.com/serpis172/Yubeos/internal/addons"
)

type addonDTO struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Icon        string   `json:"icon,omitempty"`
	Categories  []string `json:"categories,omitempty"`
	State       string   `json:"state"`
	Embed       string   `json:"embed"`
	Sidebar     bool     `json:"sidebar"`
	URL         string   `json:"url,omitempty"`
	URLBlocked  string   `json:"urlBlocked,omitempty"`
}

type addonList struct {
	Items       []addonDTO `json:"items"`
	Unavailable bool       `json:"unavailable,omitempty"`
}

func (s *Server) addonsList(w http.ResponseWriter, r *http.Request) {
	if s.opts.Addons == nil || !s.opts.Config.Addons.Enabled && !s.opts.Demo {
		writeJSON(w, http.StatusOK, addonList{Items: []addonDTO{}})
		return
	}
	list, err := s.opts.Addons.List(r.Context())
	if err != nil {
		// Helper or Docker unavailable: degrade to an empty list instead of failing the whole panel.
		s.opts.Log.Warn("addons list failed", "err", err)
		writeJSON(w, http.StatusOK, addonList{Items: []addonDTO{}, Unavailable: true})
		return
	}
	lan := "192.168.1.10" // demo: fixed mock address
	if !s.opts.Demo {
		lan = lanAddress()
	}
	out := addonList{Items: make([]addonDTO, 0, len(list))}
	for _, a := range list {
		url, blocked := addons.UIURL(r.Host, s.opts.Config.Addons.BaseDomain, lan, a)
		out.Items = append(out.Items, addonDTO{
			ID: a.ID, Name: a.Name, Version: a.Version, Description: a.Description, Icon: a.Icon,
			Categories: a.Categories, State: a.State, Embed: a.Embed, Sidebar: a.Sidebar, URL: url, URLBlocked: blocked,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// lanAddress picks a private, non-loopback IPv4 of this host as the alternate add-on address.
func lanAddress() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.IsPrivate() {
			if ip4 := n.IP.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return ""
}
