package api

import (
	"bufio"
	"crypto/sha1" //nolint:gosec
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testKey = "dGhlIHNhbXBsZSBub25jZQ==" // the RFC 6455 example key

func wsDial(t *testing.T, srv *httptest.Server, path, origin string) (net.Conn, *bufio.Reader, string) {
	t.Helper()
	c, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	req := "GET " + path + " HTTP/1.1\r\nHost: " + strings.TrimPrefix(srv.URL, "http://") + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + testKey + "\r\nSec-WebSocket-Version: 13\r\n"
	if origin != "" {
		req += "Origin: " + origin + "\r\n"
	}
	_, _ = c.Write([]byte(req + "\r\n"))
	br := bufio.NewReader(c)
	status, err := br.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	for { // swallow headers, remember the accept value
		line, err := br.ReadString('\n')
		if err != nil || line == "\r\n" {
			return c, br, status
		}
		if strings.HasPrefix(line, "Sec-WebSocket-Accept:") {
			sum := sha1.Sum([]byte(testKey + wsGUID)) //nolint:gosec
			if strings.TrimSpace(strings.TrimPrefix(line, "Sec-WebSocket-Accept:")) != base64.StdEncoding.EncodeToString(sum[:]) {
				t.Errorf("bad accept header %q", line)
			}
		}
	}
}

// readFrame reads one server frame (never masked).
func readFrame(t *testing.T, c net.Conn, br *bufio.Reader) (byte, []byte) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(4 * time.Second))
	var h [2]byte
	if _, err := io.ReadFull(br, h[:]); err != nil {
		t.Fatal(err)
	}
	n := int(h[1] & 0x7F)
	if n == 126 {
		var b [2]byte
		_, _ = io.ReadFull(br, b[:])
		n = int(binary.BigEndian.Uint16(b[:]))
	}
	p := make([]byte, n)
	if _, err := io.ReadFull(br, p); err != nil {
		t.Fatal(err)
	}
	return h[0] & 0x0F, p
}

// clientFrame builds a masked client frame.
func clientFrame(op byte, payload []byte) []byte {
	mask := [4]byte{1, 2, 3, 4}
	f := append([]byte{0x80 | op, 0x80 | byte(len(payload))}, mask[:]...)
	for i, b := range payload {
		f = append(f, b^mask[i%4])
	}
	return f
}

func TestWebSocketMetricsStream(t *testing.T) {
	srv := httptest.NewServer(phase2Handler(t, true, nil))
	defer srv.Close()
	origin := srv.URL

	c, br, status := wsDial(t, srv, "/ws/metrics?series=cpu.0.util,mem.*", origin)
	if !strings.Contains(status, "101") {
		t.Fatalf("status %q", status)
	}
	op, p := readFrame(t, c, br)
	var f struct {
		T int64              `json:"t"`
		V map[string]float64 `json:"v"`
	}
	if op != opText || json.Unmarshal(p, &f) != nil || f.T == 0 {
		t.Fatalf("frame: op=%d %s", op, p)
	}
	if _, ok := f.V["cpu.0.util"]; !ok || len(f.V) < 2 {
		t.Fatalf("series filter: %v", f.V)
	}
	if _, ok := f.V["net.eth0.rx"]; ok {
		t.Fatal("unrequested series leaked through the filter")
	}

	_, _ = c.Write(clientFrame(opPing, []byte("hi"))) // pong must echo the payload
	for {
		op, p = readFrame(t, c, br)
		if op == opPong {
			if string(p) != "hi" {
				t.Fatalf("pong payload %q", p)
			}
			break
		}
	}
	_, _ = c.Write(clientFrame(opClose, []byte{0x03, 0xE8})) // client close is echoed
	for {
		op, _ = readFrame(t, c, br)
		if op == opClose {
			return
		}
	}
}

func TestWebSocketHandshakeRefusals(t *testing.T) {
	srv := httptest.NewServer(phase2Handler(t, true, nil))
	defer srv.Close()
	if _, _, st := wsDial(t, srv, "/ws/metrics", "https://evil.example"); !strings.Contains(st, "403") {
		t.Errorf("cross-site origin: %q, want 403", st)
	}
	// plain GET without upgrade headers
	resp, err := http.Get(srv.URL + "/ws/metrics")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("plain GET = %d, want 400", resp.StatusCode)
	}
	// unauthenticated outside demo mode
	srv2 := httptest.NewServer(newHandler(false, nil))
	defer srv2.Close()
	if _, _, st := wsDial(t, srv2, "/ws/metrics", srv2.URL); !strings.Contains(st, "401") {
		t.Errorf("unauthenticated: %q, want 401", st)
	}
}

func TestWebSocketRejectsUnmaskedClientFrames(t *testing.T) {
	srv := httptest.NewServer(phase2Handler(t, true, nil))
	defer srv.Close()
	c, br, _ := wsDial(t, srv, "/ws/metrics", srv.URL)
	_, _ = c.Write([]byte{0x81, 0x01, 'x'}) // text frame without mask: protocol error
	for {
		op, p := readFrame(t, c, br)
		if op == opClose {
			if len(p) < 2 || binary.BigEndian.Uint16(p) != 1002 {
				t.Fatalf("close payload %v", p)
			}
			return
		}
	}
}
