// Package privd is the server side of the root helper: a typed, allow-listed method registry
// (docs/SECURITY.md §4.2). There is no "run this command" method and there never will be.
package privd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"regexp"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/serpis172/Yubeos/internal/priv"
)

// Class sets the timeout and the audit weight of a method.
type Class int

const (
	Read Class = iota
	Mutate
	Destructive
)

func (c Class) String() string { return [...]string{"read", "mutate", "destructive"}[c] }

func (c Class) timeout() time.Duration {
	switch c {
	case Mutate:
		return 120 * time.Second
	case Destructive:
		return 600 * time.Second
	default:
		return 10 * time.Second
	}
}

// Handler implements one method. Return *priv.Error for expected failures; anything else becomes "internal".
type Handler func(ctx context.Context, meta priv.Meta, params json.RawMessage) (any, error)

// Method is one registry entry. RootOnly methods (e.g. creating setup links for yubeosctl) are refused for
// any peer that is not uid 0, so a compromised daemon cannot call them.
type Method struct {
	Name     string
	Class    Class
	RootOnly bool
	Handle   Handler
}

var methodName = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$`)

type peerKey struct{}

// PeerUID returns the authenticated uid of the calling socket peer (or the Direct caller's configured uid).
func PeerUID(ctx context.Context) (uint32, bool) {
	u, ok := ctx.Value(peerKey{}).(uint32)
	return u, ok
}

// Server dispatches requests to registered methods.
type Server struct {
	log         *slog.Logger
	mu          sync.RWMutex
	methods     map[string]Method
	allowedUIDs map[uint32]bool
	sem         chan struct{}
}

// NewServer creates a server that only accepts socket peers whose uid is in allowedUIDs.
func NewServer(log *slog.Logger, allowedUIDs ...uint32) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{log: log, methods: map[string]Method{}, allowedUIDs: map[uint32]bool{}, sem: make(chan struct{}, 32)}
	for _, u := range allowedUIDs {
		s.allowedUIDs[u] = true
	}
	return s
}

// Register adds a method; it panics on programmer errors (bad name, duplicate, nil handler) at start-up.
func (s *Server) Register(m Method) {
	if !methodName.MatchString(m.Name) || m.Handle == nil {
		panic(fmt.Sprintf("privd: invalid method registration %q", m.Name))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.methods[m.Name]; dup {
		panic("privd: duplicate method " + m.Name)
	}
	s.methods[m.Name] = m
}

// Methods lists registered method names (for tests and `yubeosctl doctor`).
func (s *Server) Methods() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.methods))
	for n := range s.methods {
		out = append(out, n)
	}
	return out
}

// Dispatch runs one request. ctx must carry the peer uid (see Serve / Direct).
func (s *Server) Dispatch(ctx context.Context, req priv.Request) (resp priv.Response) {
	resp = priv.Response{JSONRPC: "2.0", ID: req.ID}
	fail := func(e *priv.Error) priv.Response { resp.Error = e; return resp }

	if req.JSONRPC != "2.0" {
		return fail(priv.Errorf(priv.CodeInvalidParams, "jsonrpc must be 2.0"))
	}
	s.mu.RLock()
	m, ok := s.methods[req.Method]
	s.mu.RUnlock()
	if !ok {
		return fail(priv.Errorf(priv.CodeMethodNotFound, "unknown method"))
	}
	uid, _ := PeerUID(ctx)
	if m.RootOnly && uid != 0 {
		s.log.Warn("priv: root-only method refused", "method", m.Name, "uid", uid, "reqId", req.Meta.ReqID)
		return fail(priv.Errorf(priv.CodeForbidden, "method requires a root caller"))
	}
	ctx, cancel := context.WithTimeout(ctx, m.Class.timeout())
	defer cancel()

	start := time.Now()
	defer func() {
		if p := recover(); p != nil {
			s.log.Error("priv: handler panic", "method", m.Name, "reqId", req.Meta.ReqID, "panic", p, "stack", string(debug.Stack()))
			resp = priv.Response{JSONRPC: "2.0", ID: req.ID, Error: priv.Errorf(priv.CodeInternal, "internal error")}
		}
	}()
	out, err := m.Handle(ctx, req.Meta, req.Params)
	lvl := slog.LevelDebug
	if m.Class != Read {
		lvl = slog.LevelInfo
	}
	if err != nil {
		var pe *priv.Error
		if errors.As(err, &pe) {
			s.log.Log(ctx, lvl, "priv call", "method", m.Name, "class", m.Class.String(), "reqId", req.Meta.ReqID, "actor", req.Meta.Actor, "ok", false, "code", pe.Code, "ms", time.Since(start).Milliseconds())
			return fail(pe)
		}
		s.log.Error("priv: handler error", "method", m.Name, "reqId", req.Meta.ReqID, "err", err)
		return fail(priv.Errorf(priv.CodeInternal, "internal error"))
	}
	s.log.Log(ctx, lvl, "priv call", "method", m.Name, "class", m.Class.String(), "reqId", req.Meta.ReqID, "actor", req.Meta.Actor, "ok", true, "ms", time.Since(start).Milliseconds())
	if out != nil {
		raw, err := json.Marshal(out)
		if err != nil {
			return fail(priv.Errorf(priv.CodeInternal, "internal error"))
		}
		resp.Result = raw
	}
	return resp
}

// Serve accepts connections until ctx is cancelled. Peers whose uid is not allowed are dropped without a reply.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() { <-ctx.Done(); _ = ln.Close() }()
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		uid, err := peerUID(conn)
		if err != nil || !s.allowedUIDs[uid] {
			s.log.Warn("priv: connection from unauthorised peer dropped", "uid", uid, "err", err)
			_ = conn.Close()
			continue
		}
		select {
		case s.sem <- struct{}{}:
		default:
			s.log.Warn("priv: too many concurrent connections")
			_ = conn.Close()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-s.sem }()
			s.handle(context.WithValue(ctx, peerKey{}, uid), conn)
		}()
	}
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Minute)) // ceiling; Dispatch applies the per-class timeout
	var req priv.Request
	if err := priv.ReadFrame(conn, &req); err != nil {
		if !errors.Is(err, io.EOF) {
			_ = priv.WriteFrame(conn, priv.Response{JSONRPC: "2.0", Error: priv.Errorf(priv.CodeInvalidParams, "malformed request")})
		}
		return
	}
	_ = priv.WriteFrame(conn, s.Dispatch(ctx, req))
}

// peerUID reads SO_PEERCRED of a Unix connection.
func peerUID(conn net.Conn) (uint32, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("not a unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *syscall.Ucred
	var serr error
	if err := raw.Control(func(fd uintptr) {
		cred, serr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if serr != nil {
		return 0, serr
	}
	return cred.Uid, nil
}

// Direct is an in-process priv.Caller used by demo mode and tests. It still marshals through JSON so behaviour
// matches the socket path; the caller's uid is fixed at construction.
type Direct struct {
	Server *Server
	UID    uint32
}

func (d Direct) Call(ctx context.Context, method string, meta priv.Meta, params, result any) error {
	req := priv.Request{JSONRPC: "2.0", ID: 1, Method: method, Meta: meta}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return err
		}
		req.Params = raw
	}
	resp := d.Server.Dispatch(context.WithValue(ctx, peerKey{}, d.UID), req)
	return priv.DecodeResponse(resp, result)
}

// Decode strictly decodes method params (unknown fields are errors) into T.
func Decode[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, priv.Errorf(priv.CodeInvalidParams, "params required")
	}
	dec := json.NewDecoder(bytesReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, priv.Errorf(priv.CodeInvalidParams, "invalid params: %v", err)
	}
	return v, nil
}
