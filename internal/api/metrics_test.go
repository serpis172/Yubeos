package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/serpis172/Yubeos/internal/metrics"
)

func ms(key string, v float64) metrics.Sample {
	return metrics.Sample{Meta: metrics.Meta{Key: key, Label: key, Unit: "percent", Group: "cpu", Device: "0"}, V: v}
}

func (f *flow) seed() {
	for i := 0; i < 30; i++ {
		f.metr.Put(f.clk.Now().Add(time.Duration(i-30)*time.Second), []metrics.Sample{ms("cpu.0.util", float64(i)), ms("cpu.1.util", 1), ms("mem.used", 5)})
	}
	f.metr.SetDevices("cpu", []metrics.Device{{Kind: "cpu", ID: "0", Label: "Xeon"}})
	f.metr.SetStatus(metrics.ProviderStatus{Name: "rapl", OK: false, Reason: "RAPL requires the privileged helper"})
}

func TestMetricsRESTRequiresLoginAndServesData(t *testing.T) {
	f := newFlow(t, "none", nil)
	f.enrol("bob", flowPW)
	f.seed()
	for _, p := range []string{"/api/v1/metrics/catalog", "/api/v1/metrics/snapshot", "/api/v1/metrics/history?series=cpu.0.util&range=5m"} {
		if w := f.do("GET", p, nil); w.Code != 401 {
			t.Fatalf("%s anonymous: %d", p, w.Code)
		}
	}
	s := f.mustSignIn("bob")
	cat := decode[struct {
		Series    []struct{ Key, Unit, Group string }
		Devices   []struct{ Kind, Label string }
		Providers []struct {
			Name   string
			OK     bool
			Reason string
		}
	}](t, f.do("GET", "/api/v1/metrics/catalog", nil, withCookie(s.ck)))
	if len(cat.Series) != 3 || cat.Series[0].Key != "cpu.0.util" || len(cat.Devices) != 1 || len(cat.Providers) != 1 || cat.Providers[0].OK {
		t.Fatalf("catalog: %+v", cat)
	}
	snap := decode[struct {
		Values map[string]float64
		Stale  []string
	}](t, f.do("GET", "/api/v1/metrics/snapshot", nil, withCookie(s.ck)))
	if snap.Values["cpu.0.util"] != 29 || snap.Values["mem.used"] != 5 {
		t.Fatalf("snapshot: %+v", snap)
	}
	h := decode[struct {
		Range  string
		Series map[string]struct {
			T []int64
			V []float64
		}
	}](t, f.do("GET", "/api/v1/metrics/history?series=cpu.*&range=5m", nil, withCookie(s.ck)))
	if h.Range != "5m" || len(h.Series["cpu.0.util"].V) != 30 || len(h.Series) != 2 {
		t.Fatalf("history: %+v", h)
	}
	for name, q := range map[string]string{"bad range": "series=a&range=2y", "no series": "range=5m", "bad step": "series=a&range=5m&step=x"} {
		if w := f.do("GET", "/api/v1/metrics/history?"+q, nil, withCookie(s.ck)); w.Code != 400 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	// ranges beyond the ring come from the SQL rollups
	if w := f.do("GET", "/api/v1/metrics/history?series=cpu.0.util&range=24h", nil, withCookie(s.ck)); w.Code != 200 {
		t.Fatalf("24h: %d %s", w.Code, w.Body)
	}
}

// wsDial connects to the handler through a real server, presenting Host nas.lan like a browser would.
func (f *flow) wsDial(t *testing.T, path string, hdr http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	srv := httptest.NewServer(f.h)
	t.Cleanup(srv.Close)
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, n, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, n, strings.TrimPrefix(srv.URL, "http://"))
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return websocket.Dial(ctx, "ws://nas.lan"+path, &websocket.DialOptions{HTTPClient: client, HTTPHeader: hdr})
}

func readFrame(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func cookieHdr(s sessn) http.Header {
	h := http.Header{}
	h.Set("Cookie", sessionCookie+"="+s.ck)
	return h
}

func TestMetricsWebSocketStreamsFilteredFrames(t *testing.T) {
	f := newFlow(t, "none", nil)
	f.enrol("bob", flowPW)
	f.seed()
	s := f.mustSignIn("bob")

	if _, resp, err := f.wsDial(t, "/ws/metrics", nil); err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("anonymous WebSocket must be refused with 401: %v %v", err, resp)
	}
	evil := cookieHdr(s)
	evil.Set("Origin", "https://evil.example")
	if _, _, err := f.wsDial(t, "/ws/metrics", evil); err == nil {
		t.Fatal("cross-origin WebSocket must be refused")
	}

	c, _, err := f.wsDial(t, "/ws/metrics?series=cpu.0.util,mem.*", cookieHdr(s))
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	first := readFrame(t, c)
	v := first["v"].(map[string]any)
	if v["cpu.0.util"] != float64(29) || v["mem.used"] != float64(5) || v["cpu.1.util"] != nil {
		t.Fatalf("snapshot frame must contain only the subscribed series: %v", first)
	}
	f.metr.Put(time.Now(), []metrics.Sample{ms("cpu.0.util", 77), ms("cpu.1.util", 99)})
	next := readFrame(t, c)
	nv := next["v"].(map[string]any)
	if nv["cpu.0.util"] != float64(77) || nv["cpu.1.util"] != nil {
		t.Fatalf("live frame: %v", next)
	}
}

func TestMetricsWebSocketConnectionLimitPerUser(t *testing.T) {
	f := newFlow(t, "none", nil)
	f.enrol("bob", flowPW)
	f.seed()
	s := f.mustSignIn("bob")
	var open []*websocket.Conn
	defer func() {
		for _, c := range open {
			c.CloseNow()
		}
	}()
	for i := 0; i < maxWSPerUser; i++ {
		c, _, err := f.wsDial(t, "/ws/metrics", cookieHdr(s))
		if err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
		open = append(open, c)
		readFrame(t, c)
	}
	if _, resp, err := f.wsDial(t, "/ws/metrics", cookieHdr(s)); err == nil || resp == nil || resp.StatusCode != 429 {
		t.Fatalf("connection beyond the per-user limit must get 429: %v %v", err, resp)
	}
}
