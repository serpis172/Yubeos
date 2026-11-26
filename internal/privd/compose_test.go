package privd

import "testing"

const inspectJSON = `[
 {"State":{"Status":"running"},"Config":{"Labels":{"dev.yubeos.addon":"jellyfin","dev.yubeos.name":"Jellyfin","dev.yubeos.version":"10.10.3-1","dev.yubeos.description":"Media, server","dev.yubeos.categories":"media,video","dev.yubeos.ui.port":"8096"}},
  "NetworkSettings":{"Ports":{"8096/tcp":[{"HostIp":"0.0.0.0","HostPort":"20001"}],"7359/udp":null}}},
 {"State":{"Status":"exited"},"Config":{"Labels":{"dev.yubeos.addon":"jellyfin"}},"NetworkSettings":{"Ports":{}}},
 {"State":{"Status":"exited"},"Config":{"Labels":{"dev.yubeos.addon":"Bad_ID!"}},"NetworkSettings":{"Ports":{}}},
 {"State":{"Status":"restarting"},"Config":{"Labels":{"dev.yubeos.addon":"files","dev.yubeos.ui.embed":"weird","dev.yubeos.ui.sidebar":"false"}},"NetworkSettings":{"Ports":{}}}
]`

func TestParseInspect(t *testing.T) {
	items, err := ParseInspect([]byte(inspectJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 add-ons (invalid id dropped, containers merged), got %+v", items)
	}
	j := items[0]
	if j.ID != "jellyfin" || j.State != "running" || j.HostPort != 20001 || j.ContainerPort != 8096 ||
		j.Description != "Media, server" || len(j.Categories) != 2 || j.Embed != "link" || j.Path != "/" || !j.Sidebar {
		t.Fatalf("jellyfin: %+v", j)
	}
	f := items[1]
	if f.State != "starting" || f.Embed != "link" || f.Sidebar || f.Name != "files" {
		t.Fatalf("files: %+v", f)
	}
}

func TestParseInspectRejectsGarbage(t *testing.T) {
	if _, err := ParseInspect([]byte("not json")); err == nil {
		t.Fatal("expected an error")
	}
}
