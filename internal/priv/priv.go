// Package priv is the wire protocol and client for the root helper (docs/SECURITY.md §4):
// JSON-RPC 2.0 over a Unix stream socket, 4-byte big-endian length-prefixed frames (max 1 MiB).
package priv

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"
)

// MaxFrame is the largest accepted frame.
const MaxFrame = 1 << 20

// Meta travels with every request for audit and correlation. It is informational: the helper never
// derives authority from it (authority comes from the socket peer and from proofs in params).
type Meta struct {
	ReqID string `json:"reqId,omitempty"`
	Actor string `json:"actor,omitempty"`
	Role  string `json:"role,omitempty"`
	IP    string `json:"ip,omitempty"`
}

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Meta    Meta            `json:"meta"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error is both the wire error object and the Go error returned by clients and handlers.
// Code is a stable machine-readable string (e.g. "invalid-params"); handlers return these for expected failures.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Standard codes.
const (
	CodeInvalidParams  = "invalid-params"
	CodeMethodNotFound = "method-not-found"
	CodeForbidden      = "forbidden"
	CodeInternal       = "internal"
	CodeTimeout        = "timeout"
	CodeUnavailable    = "unavailable"
	CodeTooLarge       = "too-large"
	CodeWeakPassword   = "weak-password"
	CodeInvalidToken   = "invalid-token"
	CodeNotFound       = "not-found"
)

func Errorf(code, format string, a ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, a...)}
}

// ErrUnavailable is returned by clients when the helper cannot be reached.
var ErrUnavailable = &Error{Code: CodeUnavailable, Message: "privileged helper is not reachable"}

// WriteFrame marshals v and writes it with a length prefix.
func WriteFrame(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > MaxFrame {
		return Errorf(CodeTooLarge, "frame of %d bytes exceeds %d", len(b), MaxFrame)
	}
	buf := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(buf, uint32(len(b)))
	copy(buf[4:], b)
	_, err = w.Write(buf)
	return err
}

// ReadFrame reads one frame into v. The size is validated BEFORE allocating.
func ReadFrame(r io.Reader, v any) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > MaxFrame {
		return Errorf(CodeTooLarge, "invalid frame length %d", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Caller invokes privileged methods. Implemented by Client (socket) and privd.Direct (in-process, demo/tests).
type Caller interface {
	Call(ctx context.Context, method string, meta Meta, params, result any) error
}

// Client dials the helper socket per call (simple and robust across helper restarts).
type Client struct {
	Socket  string
	Timeout time.Duration // dial + I/O ceiling when ctx has no deadline (default 30 s)
	seq     atomic.Uint64
}

func NewClient(socket string) *Client { return &Client{Socket: socket} }

func (c *Client) Call(ctx context.Context, method string, meta Meta, params, result any) error {
	to := c.Timeout
	if to == 0 {
		to = 30 * time.Second
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, to)
		defer cancel()
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.Socket)
	if err != nil {
		return ErrUnavailable
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	req := Request{JSONRPC: "2.0", ID: c.seq.Add(1), Method: method, Meta: meta}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return err
		}
		req.Params = raw
	}
	if err := WriteFrame(conn, req); err != nil {
		return ErrUnavailable
	}
	var resp Response
	if err := ReadFrame(conn, &resp); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
			return Errorf(CodeTimeout, "helper did not answer in time")
		}
		return ErrUnavailable
	}
	return decodeResponse(resp, result)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func decodeResponse(resp Response, result any) error {
	if resp.Error != nil {
		return resp.Error
	}
	if result == nil || len(resp.Result) == 0 {
		return nil
	}
	return json.Unmarshal(resp.Result, result)
}

// DecodeResponse is exported for in-process callers (privd.Direct).
func DecodeResponse(resp Response, result any) error { return decodeResponse(resp, result) }
