package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
	c.Server.TLS = TLS{Mode: "files", CertFile: "/etc/ssl/nas.pem", KeyFile: "/etc/ssl/nas.key"}
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
		"allowedHosts":        func(c *Config) { c.Server.AllowedHosts = nil },
		"trusted certificate": func(c *Config) { c.Server.TLS = TLS{Mode: "selfsigned"} },
		"auth.mode":           func(c *Config) { c.Auth.Mode = "pam" },
		"require2fa=all":      func(c *Config) { c.Auth.Require2FA = "admins" },
		"allowRoot":           func(c *Config) { c.Auth.AllowRoot = true },
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

func TestCatalogVerifyDefaultsToTrue(t *testing.T) {
	p := writeCfg(t, `{"addons":{"catalogs":[{"id":"official","url":"https://example.org/c"},{"id":"mine","url":"https://example.org/m","verify":false}]}}`)
	c, err := Load(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Addons.Catalogs[0].VerifyEnabled() {
		t.Fatal("omitted verify must default to true (signature checks must not be disabled silently)")
	}
	if c.Addons.Catalogs[1].VerifyEnabled() {
		t.Fatal("explicit verify=false must be honoured")
	}
}

func TestACMEIsRejectedUntilImplemented(t *testing.T) {
	c := Default()
	c.Server.TLS.Mode = "acme"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("acme must be rejected, got %v", err)
	}
}

func TestAllowedHostsValidation(t *testing.T) {
	good := []string{"nas.example.org", "NAS", "192.168.1.10", "::1", "[fe80::1]", "a-b.c"}
	bad := []string{"nas.example.org:8443", "https://nas", "*.example.org", "nas/x", "", "-x.org", "a..b", "x y"}
	for _, h := range good {
		if !validHostEntry(h) {
			t.Errorf("%q should be valid", h)
		}
	}
	for _, h := range bad {
		if validHostEntry(h) {
			t.Errorf("%q should be invalid", h)
		}
	}
	c := Default()
	c.Server.AllowedHosts = []string{"nas.example.org:8443"}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "allowedHosts") {
		t.Fatalf("expected allowedHosts error, got %v", err)
	}
}

func TestFilesAndTerminalValidation(t *testing.T) {
	c := Default()
	c.Files.Roots = []string{"/", "/mnt", "/mnt/"}
	c.Terminal.Shell = "bash"
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"must not contain", "duplicated", "terminal.shell"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

func TestAccessAllowsBareIP(t *testing.T) {
	c := Default()
	c.Access.AllowCIDRs = []string{"203.0.113.7", "2001:db8::/32"}
	if err := c.Validate(); err != nil {
		t.Fatalf("bare IPs must be accepted: %v", err)
	}
}

func TestWarningsFlagUnenforcedSettings(t *testing.T) {
	if w := Default().Warnings(); len(w) != 1 || !strings.Contains(w[0], "Phase 1c") {
		t.Fatalf("defaults (require2fa=admins) should only carry the 1c second-factor note: %v", w)
	}
	c := Default()
	c.Auth.Require2FA = "none"
	if w := c.Warnings(); len(w) != 0 {
		t.Fatalf("with 2FA off and defaults nothing is pending: %v", w)
	}
	c.Terminal.AllowUsers = true
	c.Auth.TrustedDeviceDays = 7
	c.Auth.Mode = "pam"
	c.Server.TrustedProxies = []string{"10.0.0.0/8"}
	joined := strings.Join(c.Warnings(), "\n")
	for _, want := range []string{"terminal.*", "trustedDeviceDays", "not implemented: native", "very broad"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings missing %q:\n%s", want, joined)
		}
	}
}

// Guards against the Go defaults drifting from schemas/config.schema.json (e.g. the catalogue verify bug).
func TestDefaultsMatchSchemaDefaults(t *testing.T) {
	raw, err := os.ReadFile("../../schemas/config.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	def, _ := json.Marshal(Default())
	var cfg map[string]any
	if err := json.Unmarshal(def, &cfg); err != nil {
		t.Fatal(err)
	}
	var walk func(path string, props map[string]any, val map[string]any)
	walk = func(path string, props map[string]any, val map[string]any) {
		for name, p := range props {
			pm, _ := p.(map[string]any)
			got, present := val[name]
			if sub, ok := pm["properties"].(map[string]any); ok {
				gm, _ := got.(map[string]any)
				walk(path+name+".", sub, gm)
				continue
			}
			want, hasDefault := pm["default"]
			if !hasDefault {
				continue
			}
			if !present {
				t.Errorf("%s%s: in schema with default %v but missing from Go config", path, name, want)
				continue
			}
			if !reflect.DeepEqual(normalizeEmpty(want), normalizeEmpty(got)) {
				t.Errorf("%s%s: schema default %v != Go default %v", path, name, want, got)
			}
		}
	}
	walk("", schema["properties"].(map[string]any), cfg)
}

func normalizeEmpty(v any) any {
	if s, ok := v.([]any); ok && len(s) == 0 {
		return nil
	}
	return v
}

func TestLoadYAMLWithSameStrictnessAsJSON(t *testing.T) {
	yamlDoc := `
# comment
server:
  listen: ":9443"
  allowedHosts: [nas.example.org, 192.168.1.10]
  tls: { mode: selfsigned }
auth:
  require2fa: all
  trustedDeviceDays: 14
files:
  roots:
    - /mnt
    - /srv/media
logging: { level: debug }
`
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(yamlDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Listen != ":9443" || len(c.Server.AllowedHosts) != 2 || c.Auth.Require2FA != "all" || c.Auth.TrustedDeviceDays != 14 ||
		len(c.Files.Roots) != 2 || c.Logging.Level != "debug" || c.Auth.AdminGroup != "yubeos-admin" {
		t.Fatalf("YAML not decoded onto defaults correctly: %+v", c)
	}
	bad := map[string]string{
		"unknown key":   "server:\n  lisen: ':1'\n",
		"duplicate key": "server:\n  listen: ':1'\n  listen: ':2'\n",
		"wrong type":    "auth:\n  sessionIdleMinutes: soon\n",
		"not a mapping": "- a\n- b\n",
		"broken indent": "server:\n listen: ':1'\n   bad: 2\n",
	}
	for name, doc := range bad {
		q := filepath.Join(t.TempDir(), "bad.yaml")
		_ = os.WriteFile(q, []byte(doc), 0o600)
		if _, err := Load(q, nil); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}

func TestEmptyConfigFileMeansDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(p, []byte("\n# nothing here\n"), 0o600)
	c, err := Load(p, nil)
	if err != nil || c.Server.Listen != ":443" {
		t.Fatalf("%v %+v", err, c.Server)
	}
}
