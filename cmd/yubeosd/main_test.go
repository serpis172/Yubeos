package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunVersion(t *testing.T) {
	if code := run([]string{"--version"}, nil); code != 0 {
		t.Fatalf("exit %d", code)
	}
}

func TestRunRejectsNoTLSOutsideDemo(t *testing.T) {
	if code := run([]string{"--no-tls"}, nil); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestRunRejectsBadConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(p, []byte(`{"server":{"nope":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--config", p}, nil); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if code := run([]string{"--config", "/does/not/exist.json"}, nil); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestRunRejectsUnknownFlag(t *testing.T) {
	if code := run([]string{"--bogus"}, nil); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}
