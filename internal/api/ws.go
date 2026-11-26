package api

import (
	"bufio"
	"crypto/sha1" //nolint:gosec // mandated by RFC 6455 for the handshake, not a security use
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/serpis172/Yubeos/internal/problem"
)

// A deliberately small RFC 6455 server: text frames out, ping/pong/close in. No extensions, no fragmentation
// of our own frames, no compression. Enough for server-pushed JSON (metrics today, jobs/terminal later).

const (
	wsGUID        = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	wsMaxIn       = 64 << 10 // clients only ever send control frames; anything big is a protocol abuse
	wsWriteWait   = 10 * time.Second
	wsPingEvery   = 25 * time.Second
	wsReadTimeout = 70 * time.Second
	wsMaxConns    = 64

	opCont, opText, opBinary, opClose, opPing, opPong = 0x0, 0x1, 0x2, 0x8, 0x9, 0xA
)

type wsConn struct {
	c  net.Conn
	br *bufio.Reader
	mu sync.Mutex // serialises writes (data frames and pongs)
}

func headerHas(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// wsUpgrade validates the handshake and takes over the connection. On failure it has already answered.
func wsUpgrade(w http.ResponseWriter, r *http.Request, originOK func(*http.Request) bool) (*wsConn, bool) {
	key := r.Header.Get("Sec-WebSocket-Key")
	switch {
	case r.Method != http.MethodGet || !headerHas(r.Header, "Connection", "upgrade") || !headerHas(r.Header, "Upgrade", "websocket"):
		problem.Write(w, r, http.StatusBadRequest, "bad-request", "WebSocket upgrade required", "")
		return nil, false
	case r.Header.Get("Sec-WebSocket-Version") != "13":
		w.Header().Set("Sec-WebSocket-Version", "13")
		problem.Write(w, r, http.StatusUpgradeRequired, "bad-request", "Unsupported WebSocket version", "")
		return nil, false
	case !originOK(r): // cross-site WebSocket hijacking guard (docs/AUTHENTICATION.md §6)
		problem.Write(w, r, http.StatusForbidden, "csrf", "Cross-site request refused", "Origin check failed")
		return nil, false
	}
	if raw, err := base64.StdEncoding.DecodeString(key); err != nil || len(raw) != 16 {
		problem.Write(w, r, http.StatusBadRequest, "bad-request", "Invalid Sec-WebSocket-Key", "")
		return nil, false
	}
	conn, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		problem.Write(w, r, http.StatusInternalServerError, "internal", "WebSocket not supported here", "")
		return nil, false
	}
	sum := sha1.Sum([]byte(key + wsGUID)) //nolint:gosec
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " +
		base64.StdEncoding.EncodeToString(sum[:]) + "\r\n\r\n"
	_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
	if _, err := rw.WriteString(resp); err != nil || rw.Flush() != nil {
		_ = conn.Close()
		return nil, false
	}
	return &wsConn{c: conn, br: rw.Reader}, true
}

func (w *wsConn) Close() { _ = w.c.Close() }

// write sends one unfragmented frame (server frames are never masked).
func (w *wsConn) write(op byte, payload []byte) error {
	hdr := []byte{0x80 | op}
	switch n := len(payload); {
	case n <= 125:
		hdr = append(hdr, byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 126, 0, 0)
		binary.BigEndian.PutUint16(hdr[2:], uint16(n))
	default:
		hdr = append(hdr, 127, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.BigEndian.PutUint64(hdr[2:], uint64(n))
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.c.SetWriteDeadline(time.Now().Add(wsWriteWait))
	if _, err := w.c.Write(append(hdr, payload...)); err != nil {
		return err
	}
	return nil
}

func (w *wsConn) closeWith(code uint16) {
	p := make([]byte, 2)
	binary.BigEndian.PutUint16(p, code)
	_ = w.write(opClose, p)
	w.Close()
}

var errWSClosed = errors.New("websocket closed")

// readLoop consumes client frames until the peer closes or misbehaves, answering pings. It returns when the
// connection is done; the caller then stops writing.
func (w *wsConn) readLoop() error {
	for {
		_ = w.c.SetReadDeadline(time.Now().Add(wsReadTimeout))
		var h [2]byte
		if _, err := io.ReadFull(w.br, h[:]); err != nil {
			return err
		}
		fin, op, masked, n := h[0]&0x80 != 0, h[0]&0x0F, h[1]&0x80 != 0, uint64(h[1]&0x7F)
		if h[0]&0x70 != 0 || !masked { // reserved bits (no extensions negotiated) / unmasked client frame
			w.closeWith(1002)
			return errors.New("websocket protocol error")
		}
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(w.br, b[:]); err != nil {
				return err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(w.br, b[:]); err != nil {
				return err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if op >= opClose && (n > 125 || !fin) { // control frames: short and unfragmented
			w.closeWith(1002)
			return errors.New("websocket protocol error")
		}
		if n > wsMaxIn {
			w.closeWith(1009)
			return errors.New("websocket frame too large")
		}
		var mask [4]byte
		if _, err := io.ReadFull(w.br, mask[:]); err != nil {
			return err
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(w.br, payload); err != nil {
			return err
		}
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
		switch op {
		case opPing:
			if err := w.write(opPong, payload); err != nil {
				return err
			}
		case opClose:
			_ = w.write(opClose, payload) // echo the status code
			w.Close()
			return errWSClosed
		}
		// text, binary, continuation and pong frames carry nothing we act on
	}
}
