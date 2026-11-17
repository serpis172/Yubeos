package audit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/serpis172/Yubeos/internal/clock"
	"github.com/serpis172/Yubeos/internal/db"
)

func newLog(t *testing.T) (*Log, *db.DB) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "y.db"), db.Migrations(), log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return New(d, clock.NewFake(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)), log), d
}

func TestChainAppendAndVerify(t *testing.T) {
	l, _ := newLog(t)
	ctx := context.Background()
	for _, a := range []string{"auth.login.ok", "auth.login.fail", "auth.logout"} {
		if err := l.Append(ctx, Entry{Actor: "alice", Action: a, Result: OK, IP: "10.0.0.2", Params: map[string]any{"password": "hunter2", "note": "x"}}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := l.Verify(ctx)
	if err != nil || n != 3 {
		t.Fatalf("verify: n=%d err=%v", n, err)
	}
}

func TestVerifyDetectsTamperingAndDeletion(t *testing.T) {
	l, d := newLog(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		_ = l.Append(ctx, Entry{Actor: "a", Action: "x", Result: OK})
	}
	if _, err := d.Exec(`UPDATE audit_log SET actor='mallory' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Verify(ctx); !errors.Is(err, ErrChainBroken) {
		t.Fatalf("modified row not detected: %v", err)
	}
	// restore, then delete a middle row
	_, _ = d.Exec(`UPDATE audit_log SET actor='a' WHERE id=2`)
	if _, err := l.Verify(ctx); err != nil {
		t.Fatalf("restored chain should verify: %v", err)
	}
	if _, err := d.Exec(`DELETE FROM audit_log WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Verify(ctx); !errors.Is(err, ErrChainBroken) {
		t.Fatalf("deleted row not detected: %v", err)
	}
}

func TestSecretsNeverStored(t *testing.T) {
	l, d := newLog(t)
	_ = l.Append(context.Background(), Entry{Action: "auth.password.change", Result: OK,
		Params: map[string]any{"newPassword": "S3cret!", "totpCode": "123456", "user": "bob"}})
	var p string
	if err := d.QueryRow(`SELECT params FROM audit_log`).Scan(&p); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"S3cret!", "123456"} {
		if contains(p, bad) {
			t.Fatalf("secret %q leaked into audit params: %s", bad, p)
		}
	}
	if !contains(p, "bob") {
		t.Fatalf("non-secret param lost: %s", p)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool { return indexOf(s, sub) >= 0 })()
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestInvalidEntriesRejected(t *testing.T) {
	l, _ := newLog(t)
	if l.Append(context.Background(), Entry{Action: "", Result: OK}) == nil || l.Append(context.Background(), Entry{Action: "x", Result: "maybe"}) == nil {
		t.Fatal("invalid entries must be rejected")
	}
}
