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
		"YUBEOS_AUTH_REQUIRE2FA=all",
		"YUBEOS_AUTH_SESSIONIDLEMINUTES=45",
		"YUBEOS_FILES_ROOTS=/mnt, /srv",
		"UNRELATED=1",
	}
	c, err := Load("", env)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Listen != ":8443" || c.Auth.Require2FA != "all" || c.Auth.SessionIdleMinutes != 45 {
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

func remoteOK() Config {
	c := Default()
	c.Server.Exposure = "remote"
	c.Server.AllowedHosts = []string{"nas.example.org"}
	c.Server.TLS = TLS{Mode: "acme", ACMEDomains: []string{"nas.example.org"}}
	c.Auth.Require2FA = "all"
	return c
}

func TestRemoteExposureValidConfig(t *testing.T) {
	if err := remoteOK().Validate(); err != nil {
		t.Fatalf("valid remote config rejected: %v", err)
	}
}

func TestRemoteExposureHardRules(t *testing.T) {
	cases := map[string]func(*Config){
		"allowedHosts":   func(c *Config) { c.Server.AllowedHosts = nil },
		"trusted cert":   func(c *Config) { c.Server.TLS = TLS{Mode: "selfsigned"} },
		"auth.mode":      func(c *Config) { c.Auth.Mode = "pam" },
		"require2fa=all": func(c *Config) { c.Auth.Require2FA = "admins" },
		"allowRoot":      func(c *Config) { c.Auth.AllowRoot = true },
	}
	for want, mutate := range cases {
		c := remoteOK()
		mutate(&c)
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("mutation %q: expected error mentioning it, got %v", want, err)
		}
	}
}

func TestBehindProxyAllowsNoTLSCertButNeedsTrustedProxies(t *testing.T) {
	c := remoteOK()
	c.Server.TLS = TLS{Mode: "selfsigned"}
	c.Server.BehindProxy = true
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "trustedProxies") {
		t.Fatalf("expected trustedProxies error, got %v", err)
	}
	c.Server.TrustedProxies = []string{"127.0.0.1", "10.0.0.0/8"}
	if err := c.Validate(); err != nil {
		t.Fatalf("behindProxy with trusted proxies should be valid: %v", err)
	}
	c.Server.TrustedProxies = []string{"not-an-ip"}
	if err := c.Validate(); err == nil {
		t.Fatal("invalid proxy entry accepted")
	}
}

func TestAuthBounds(t *testing.T) {
	c := Default()
	c.Auth.TrustedDeviceDays = 91
	c.Auth.LockoutThreshold = 1
	c.Auth.Require2FA = "sometimes"
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"trustedDeviceDays", "lockoutThreshold", "require2fa"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

func TestAccessAndRemoteIdleValidation(t *testing.T) {
	c := Default()
	c.Access.AllowCIDRs = []string{"203.0.113.0/24", "nope"}
	c.Auth.RemoteIdleMinutes = c.Auth.SessionIdleMinutes + 1
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "allowCidrs") || !strings.Contains(err.Error(), "remoteIdleMinutes") {
		t.Fatalf("expected both errors, got %v", err)
	}
	c = Default()
	c.Access.AllowCIDRs = []string{"203.0.113.0/24"}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid access config rejected: %v", err)
	}
}

func TestSecureDefaults(t *testing.T) {
	c := Default()
	if c.Server.Exposure != "lan" || c.Auth.Mode != "native" || c.Auth.Require2FA != "admins" ||
		c.Auth.TrustedDeviceDays != 30 || !c.Auth.AllowPasskeyAsSoleFactor || !c.Terminal.RequireStepUpWhenRemote ||
		c.Auth.AllowRoot || c.Server.TLS.Mode != "selfsigned" || !c.Server.TreatOverlayAsLocal {
		t.Fatalf("insecure or unexpected defaults: %+v", c)
	}
}
