package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func runCtl(args ...string) (int, string, string) {
	var out, errw bytes.Buffer
	code := run(args, &out, &errw)
	return code, out.String(), errw.String()
}

func TestUsageAndVersion(t *testing.T) {
	if code, out, _ := runCtl("version"); code != 0 || !strings.Contains(out, "yubeosctl") {
		t.Fatalf("version: %d %q", code, out)
	}
	if code, _, errw := runCtl(); code != 2 || !strings.Contains(errw, "Usage") {
		t.Fatalf("no args: %d %q", code, errw)
	}
	if code, _, errw := runCtl("frobnicate"); code != 2 || !strings.Contains(errw, "unknown command") {
		t.Fatalf("unknown: %d %q", code, errw)
	}
	if code, out, _ := runCtl("help"); code != 0 || !strings.Contains(out, "user add") {
		t.Fatalf("help: %d", code)
	}
}

func TestUserCommandValidation(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "none.sock")
	cases := map[string][]string{
		"missing name":       {"user", "add"},
		"bad action":         {"user", "delete", "bob"},
		"invalid account":    {"user", "add", "../etc", "--socket", sock},
		"no such linux user": {"user", "add", "definitely-not-a-user-xyz", "--socket", sock},
	}
	for name, args := range cases {
		if code, _, _ := runCtl(args...); code == 0 {
			t.Errorf("%s must fail", name)
		}
	}
	// an existing account but no helper: a clear, non-zero failure
	code, _, errw := runCtl("user", "add", "root", "--socket", sock)
	if code == 0 || !strings.Contains(errw, "helper") {
		t.Fatalf("helper missing must be explained: %d %q", code, errw)
	}
}

func TestDoctorFailsWithoutHelperAndReportsEachCheck(t *testing.T) {
	d := t.TempDir()
	code, out, _ := runCtl("doctor", "--socket", filepath.Join(d, "x.sock"), "--priv-state-dir", d, "--state-dir", d)
	if code != 1 || !strings.Contains(out, "helper") || !strings.Contains(out, "configuration") {
		t.Fatalf("doctor: %d\n%s", code, out)
	}
}

func TestAuditVerifyNeedsADatabase(t *testing.T) {
	if code, _, _ := runCtl("audit", "verify", "--state-dir", t.TempDir()); code != 1 {
		t.Fatal("missing database must be an error")
	}
	if code, _, _ := runCtl("audit"); code != 2 {
		t.Fatal("usage error expected")
	}
}
