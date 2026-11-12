package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
}

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadOverlaysFileOnDefaults(t *testing.T) {
	p := writeCfg(t, `{"server":{"listen":":9443"},"logging":{"level":"debug"}}`)
	c, err := Load(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Listen != ":9443" || c.Logging.Level != "debug" {
		t.Fatalf("file values not applied: %+v", c)
	}
	if c.Server.TLS.Mode != "selfsigned" || c.Auth.AdminGroup != "yubeos-admin" {
		t.Fatalf("defaults lost: %+v", c)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	p := writeCfg(t, `{"server":{"lisen":":1"}}`)
	if _, err := Load(p, nil); err == nil || !strings.Contains(err.Error(), "lisen") {
		t.Fatalf("expected unknown-field error, got %v", err)
	}
}

func TestEnvOverridesAndTypes(t *testing.T) {
	env := []string{
		"YUBEOS_SERVER_LISTEN=:8443",
		"YUBEOS_SERVER_TLS_MODE=files",
		"YUBEOS_SERVER_TLS_CERTFILE=/c.pem",
		"YUBEOS_SERVER_TLS_KEYFILE=/k.pem",
		"YUBEOS_AUTH_REQUIRE2FAFORADMINS=true",
		"YUBEOS_AUTH_SESSIONIDLEMINUTES=45",
		"YUBEOS_FILES_ROOTS=/mnt, /srv",
		"UNRELATED=1",
	}
	c, err := Load("", env)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Listen != ":8443" || !c.Auth.Require2FAForAdmins || c.Auth.SessionIdleMinutes != 45 {
		t.Fatalf("env not applied: %+v", c)
	}
	if len(c.Files.Roots) != 2 || c.Files.Roots[1] != "/srv" {
		t.Fatalf("list parsing: %v", c.Files.Roots)
	}
}

func TestEnvBadValue(t *testing.T) {
	if _, err := Load("", []string{"YUBEOS_AUTH_SESSIONIDLEMINUTES=abc"}); err == nil {
		t.Fatal("expected error for non-integer")
	}
}

func TestValidateReportsAllProblems(t *testing.T) {
	c := Default()
	c.Server.TLS.Mode = "files"
	c.Auth.UserGroup = c.Auth.AdminGroup
	c.Files.Roots = []string{"relative"}
	c.Addons.PortRange = "80-90"
	c.Logging.Level = "loud"
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"certFile", "different", "absolute", "portRange", "logging.level"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}
