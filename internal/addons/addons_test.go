package addons

import (
	"context"
	"testing"
)

func TestUIURL(t *testing.T) {
	a := Addon{ID: "jellyfin", Embed: "link", Path: "/", HostPort: 20001}
	cases := []struct {
		name, reqHost, base, lan, wantURL, wantBlocked string
		addon                                          Addon
	}{
		{"isolated subdomain", "nas.lan", "addons.nas.example.org", "", "https://jellyfin.addons.nas.example.org/", "", a},
		{"direct link via LAN IP", "nas.lan", "", "192.168.1.10", "http://192.168.1.10:20001/", "", a},
		{"same host refused", "192.168.1.10:8443", "", "192.168.1.10", "", "same-host", a},
		{"no alternate address", "nas.lan", "", "", "", "no-alternate-address", a},
		{"no published port", "nas.lan", "", "192.168.1.10", "", "no-port", Addon{ID: "x", Embed: "link", Path: "/"}},
		{"path cannot change host", "nas.lan", "", "192.168.1.10", "http://192.168.1.10:20001/", "", Addon{ID: "x", Embed: "link", Path: "//evil.example", HostPort: 20001}},
		{"embed none has no link", "nas.lan", "", "192.168.1.10", "", "", Addon{ID: "x", Embed: "none", HostPort: 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			url, blocked := UIURL(c.reqHost, c.base, c.lan, c.addon)
			if url != c.wantURL || blocked != c.wantBlocked {
				t.Fatalf("got (%q,%q) want (%q,%q)", url, blocked, c.wantURL, c.wantBlocked)
			}
		})
	}
}

func TestDemoList(t *testing.T) {
	list, err := Demo{}.List(context.Background())
	if err != nil || len(list) == 0 {
		t.Fatalf("demo: %v %d", err, len(list))
	}
}
