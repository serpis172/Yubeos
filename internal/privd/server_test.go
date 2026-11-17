package privd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/serpis172/Yubeos/internal/priv"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type echoParams struct {
	Msg string `json:"msg"`
}

func testServer(t *testing.T, uids ...uint32) (*Server, string, context.CancelFunc) {
	t.Helper()
	s := NewServer(quiet, uids...)
	s.Register(Method{Name: "test.echo", Class: Read, Handle: func(ctx context.Context, m priv.Meta, raw json.RawMessage) (any, error) {
		p, err := Decode[echoParams](raw)
		if err != nil {
			return nil, err
		}
		return map[string]string{"msg": p.Msg, "actor": m.Actor}, nil
	}})
	s.Register(Method{Name: "test.rootonly", Class: Mutate, RootOnly: true, Handle: func(context.Context, priv.Meta, json.RawMessage) (any, error) {
		return map[string]bool{"ok": true}, nil
	}})
	s.Register(Method{Name: "test.panic", Class: Read, Handle: func(context.Context, priv.Meta, json.RawMessage) (any, error) { panic("kaboom") }})
	s.Register(Method{Name: "test.fail", Class: Read, Handle: func(context.Context, priv.Meta, json.RawMessage) (any, error) {
		return nil, priv.Errorf("custom-code", "nope")
	}})
	s.Register(Method{Name: "test.leak", Class: Read, Handle: func(context.Context, priv.Meta, json.RawMessage) (any, error) {
		return nil, errors.New("secret path /etc/shadow")
	}})
	sock := filepath.Join(t.TempDir(), "p.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = s.Serve(ctx, ln) }()
	return s, sock, cancel
}

func TestSocketRoundTripAndErrors(t *testing.T) {
	uid := uint32(os.Getuid())
	_, sock, cancel := testServer(t, uid)
	defer cancel()
	c := priv.NewClient(sock)
	ctx := context.Background()

	var out map[string]string
	if err := c.Call(ctx, "test.echo", priv.Meta{Actor: "alice"}, echoParams{Msg: "hi"}, &out); err != nil || out["msg"] != "hi" || out["actor"] != "alice" {
		t.Fatalf("echo: %v %v", err, out)
	}
	cases := []struct {
		method string
		params any
		code   string
	}{
		{"nope.missing", nil, priv.CodeMethodNotFound},
		{"test.echo", map[string]any{"msg": "x", "extra": 1}, priv.CodeInvalidParams}, // unknown field
		{"test.echo", nil, priv.CodeInvalidParams},                                    // params required
		{"test.fail", nil, "custom-code"},
		{"test.panic", nil, priv.CodeInternal},
		{"test.leak", nil, priv.CodeInternal},
	}
	for _, tc := range cases {
		err := c.Call(ctx, tc.method, priv.Meta{}, tc.params, nil)
		var pe *priv.Error
		if !errors.As(err, &pe) || pe.Code != tc.code {
			t.Errorf("%s: got %v, want code %s", tc.method, err, tc.code)
		}
		if err != nil && strings.Contains(err.Error(), "/etc/shadow") {
			t.Errorf("%s: internal error text leaked to the caller", tc.method)
		}
	}
}

func TestRootOnlyMethodRefusedForNonRootPeer(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root")
	}
	_, sock, cancel := testServer(t, uint32(os.Getuid()))
	defer cancel()
	err := priv.NewClient(sock).Call(context.Background(), "test.rootonly", priv.Meta{}, nil, nil)
	var pe *priv.Error
	if !errors.As(err, &pe) || pe.Code != priv.CodeForbidden {
		t.Fatalf("non-root peer must be refused, got %v", err)
	}
	// the same method through Direct as uid 0 works
	s, _, cancel2 := testServer(t, 0)
	defer cancel2()
	if err := (Direct{Server: s, UID: 0}).Call(context.Background(), "test.rootonly", priv.Meta{}, nil, nil); err != nil {
		t.Fatalf("root caller refused: %v", err)
	}
}

func TestUnauthorisedPeerIsDropped(t *testing.T) {
	_, sock, cancel := testServer(t, 4242) // our uid is not 4242
	defer cancel()
	err := priv.NewClient(sock).Call(context.Background(), "test.echo", priv.Meta{}, echoParams{Msg: "x"}, nil)
	if !errors.Is(err, priv.ErrUnavailable) {
		t.Fatalf("expected the connection to be dropped, got %v", err)
	}
}

func TestClientWithoutHelper(t *testing.T) {
	err := priv.NewClient(filepath.Join(t.TempDir(), "absent.sock")).Call(context.Background(), "x.y", priv.Meta{}, nil, nil)
	if !errors.Is(err, priv.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestOversizedFrameIsRejectedBeforeAllocation(t *testing.T) {
	_, sock, cancel := testServer(t, uint32(os.Getuid()))
	defer cancel()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Write([]byte{0x7f, 0xff, 0xff, 0xff}) // claims a 2 GiB frame
	var resp priv.Response
	if err := priv.ReadFrame(conn, &resp); err != nil || resp.Error == nil || resp.Error.Code != priv.CodeInvalidParams {
		t.Fatalf("expected an error reply for the bogus frame, got %v %+v", err, resp)
	}
}

func TestRegisterPanicsOnBadOrDuplicateNames(t *testing.T) {
	s := NewServer(quiet)
	h := func(context.Context, priv.Meta, json.RawMessage) (any, error) { return nil, nil }
	for _, name := range []string{"", "nodots", "Bad.Name", "a..b", "exec"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("name %q should panic", name)
				}
			}()
			s.Register(Method{Name: name, Handle: h})
		}()
	}
	s.Register(Method{Name: "ok.name", Handle: h})
	defer func() {
		if recover() == nil {
			t.Error("duplicate registration should panic")
		}
	}()
	s.Register(Method{Name: "ok.name", Handle: h})
}

func TestDirectMatchesSocketBehaviour(t *testing.T) {
	s, _, cancel := testServer(t)
	defer cancel()
	var out map[string]string
	if err := (Direct{Server: s, UID: 1000}).Call(context.Background(), "test.echo", priv.Meta{Actor: "bob"}, echoParams{Msg: "d"}, &out); err != nil || out["actor"] != "bob" {
		t.Fatalf("direct: %v %v", err, out)
	}
}
