// Package config loads, overrides and validates the Yubeos configuration.
//
// Precedence (low → high): built-in defaults < config file < YUBEOS_* environment < command-line flags.
// The file may be YAML or JSON (JSON is detected by a leading "{"); unknown keys are rejected in both.
package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

type Config struct {
	Server     Server     `json:"server"`
	Auth       Auth       `json:"auth"`
	Providers  Providers  `json:"providers"`
	Files      Files      `json:"files"`
	Terminal   Terminal   `json:"terminal"`
	Addons     Addons     `json:"addons"`
	RepoPapers RepoPapers `json:"repopapers"`
	Logging    Logging    `json:"logging"`
	Access     Access     `json:"access"`
}

// Access restricts which client networks may reach the login at all (applied before any auth logic).
type Access struct {
	AllowCIDRs []string `json:"allowCidrs"` // empty = no restriction; local origins are always allowed
}

type Server struct {
	Listen              string   `json:"listen"`
	HTTPRedirect        string   `json:"httpRedirect"`
	AllowedHosts        []string `json:"allowedHosts"`
	Exposure            string   `json:"exposure"`            // lan | remote (docs/REMOTE-ACCESS.md)
	BehindProxy         bool     `json:"behindProxy"`         // TLS terminated by a tunnel/reverse proxy in front of yubeosd
	TrustedProxies      []string `json:"trustedProxies"`      // IPs/CIDRs allowed to set X-Forwarded-For
	TreatOverlayAsLocal bool     `json:"treatOverlayAsLocal"` // classify 100.64.0.0/10 (Tailscale/CGNAT) as local
	TLS                 TLS      `json:"tls"`
}

type TLS struct {
	Mode        string   `json:"mode"` // selfsigned | files | acme
	CertFile    string   `json:"certFile"`
	KeyFile     string   `json:"keyFile"`
	ACMEDomains []string `json:"acmeDomains"`
	ACMEEmail   string   `json:"acmeEmail"`
}

type Auth struct {
	Mode                         string `json:"mode"` // native (Yubeos credentials) | pam (LAN only)
	AdminGroup                   string `json:"adminGroup"`
	UserGroup                    string `json:"userGroup"`
	AllowRoot                    bool   `json:"allowRoot"`
	SessionIdleMinutes           int    `json:"sessionIdleMinutes"`
	SessionMaxHours              int    `json:"sessionMaxHours"`
	Require2FA                   string `json:"require2fa"`        // none | admins | all
	TrustedDeviceDays            int    `json:"trustedDeviceDays"` // 0 disables "remember this device"
	AllowPasskeyAsSoleFactor     bool   `json:"allowPasskeyAsSoleFactor"`
	LockoutThreshold             int    `json:"lockoutThreshold"`
	LockoutMinutes               int    `json:"lockoutMinutes"`
	RemoteIdleMinutes            int    `json:"remoteIdleMinutes"`
	AdminRemoteTrustedDeviceDays int    `json:"adminRemoteTrustedDeviceDays"`
	BreachCheck                  bool   `json:"breachCheck"` // k-anonymity HIBP lookup when a password is set (outbound call)
}

type Providers struct {
	Storage string `json:"storage"` // auto | zfs | plain | mock
	GPU     string `json:"gpu"`     // auto | nvidia | amd | none | mock
	Power   string `json:"power"`   // auto | rapl | none | mock
}

type Files struct {
	Roots          []string `json:"roots"`
	FollowSymlinks bool     `json:"followSymlinks"`
}

type Terminal struct {
	AllowUsers              bool   `json:"allowUsers"`
	RequireStepUpWhenRemote bool   `json:"requireStepUpWhenRemote"`
	Shell                   string `json:"shell"`
	MaxSessionsPerUser      int    `json:"maxSessionsPerUser"`
	IdleTimeoutMinutes      int    `json:"idleTimeoutMinutes"`
	DetachedTTLMinutes      int    `json:"detachedTtlMinutes"`
	RecordSessions          bool   `json:"recordSessions"`
}

type Addons struct {
	Enabled    bool      `json:"enabled"`
	PortRange  string    `json:"portRange"`
	BaseDomain string    `json:"baseDomain"` // isolated origin for add-on UIs: <id>.<baseDomain> (ADR-0018)
	Catalogs   []Catalog `json:"catalogs"`
}

type Catalog struct {
	ID  string `json:"id"`
	URL string `json:"url"`
	// Verify is a pointer so that "omitted" (nil) can default to TRUE: signature verification must never be
	// silently disabled by leaving a key out. Use VerifyEnabled().
	Verify *bool `json:"verify"`
}

// VerifyEnabled reports whether catalogue signatures must be verified (default true).
func (c Catalog) VerifyEnabled() bool { return c.Verify == nil || *c.Verify }

type RepoPapers struct {
	Enabled         bool     `json:"enabled"`
	GithubTokenFile string   `json:"githubTokenFile"`
	Sources         []string `json:"sources"`
}

type Logging struct {
	Level  string `json:"level"`  // debug | info | warn | error
	Format string `json:"format"` // json | text
}

// Default returns the built-in defaults (identical to the schema defaults).
func Default() Config {
	return Config{
		Server: Server{Listen: ":443", HTTPRedirect: ":80", Exposure: "lan", TreatOverlayAsLocal: true, TLS: TLS{Mode: "selfsigned"}},
		Auth: Auth{
			Mode: "native", AdminGroup: "yubeos-admin", UserGroup: "yubeos-user",
			SessionIdleMinutes: 30, SessionMaxHours: 12,
			Require2FA: "admins", TrustedDeviceDays: 30, AllowPasskeyAsSoleFactor: true,
			LockoutThreshold: 10, LockoutMinutes: 15, RemoteIdleMinutes: 15, AdminRemoteTrustedDeviceDays: 14,
		},
		Providers: Providers{Storage: "auto", GPU: "auto", Power: "auto"},
		Files:     Files{Roots: []string{"/mnt"}},
		Terminal:  Terminal{MaxSessionsPerUser: 5, DetachedTTLMinutes: 30, RequireStepUpWhenRemote: true},
		Addons:    Addons{Enabled: true, PortRange: "20000-29999"},
		RepoPapers: RepoPapers{
			Enabled: true,
			Sources: []string{"github", "arxiv", "semanticscholar", "huggingface"},
		},
		Logging: Logging{Level: "info", Format: "json"},
	}
}

// Load starts from Default, overlays the file at path (if non-empty), then the environment, then validates.
func Load(path string, environ []string) (Config, error) {
	c := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return c, fmt.Errorf("read config: %w", err)
		}
		if err := decodeConfig(data, &c); err != nil {
			return c, fmt.Errorf("parse config %s: %w", path, err)
		}
	}
	if err := ApplyEnv(&c, environ); err != nil {
		return c, err
	}
	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}

// ApplyEnv overrides fields from YUBEOS_<PATH> variables, where PATH is the json-tag path upper-cased
// and joined by "_" (e.g. YUBEOS_SERVER_LISTEN, YUBEOS_SERVER_TLS_MODE, YUBEOS_AUTH_REQUIRE2FA).
// Lists are comma-separated. Only string, bool, int and []string fields are supported.
func ApplyEnv(c *Config, environ []string) error {
	env := make(map[string]string, len(environ))
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(k, "YUBEOS_") {
			env[k] = v
		}
	}
	return applyEnv(reflect.ValueOf(c).Elem(), "YUBEOS", env)
}

func applyEnv(v reflect.Value, prefix string, env map[string]string) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		name := strings.ToUpper(strings.Split(t.Field(i).Tag.Get("json"), ",")[0])
		key := prefix + "_" + name
		f := v.Field(i)
		if f.Kind() == reflect.Struct {
			if err := applyEnv(f, key, env); err != nil {
				return err
			}
			continue
		}
		raw, ok := env[key]
		if !ok {
			continue
		}
		switch f.Kind() {
		case reflect.String:
			f.SetString(raw)
		case reflect.Bool:
			b, err := strconv.ParseBool(raw)
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			f.SetBool(b)
		case reflect.Int:
			n, err := strconv.Atoi(raw)
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			f.SetInt(int64(n))
		case reflect.Slice:
			if f.Type().Elem().Kind() != reflect.String {
				return fmt.Errorf("%s: unsupported slice type", key)
			}
			var parts []string
			for _, p := range strings.Split(raw, ",") {
				if p = strings.TrimSpace(p); p != "" {
					parts = append(parts, p)
				}
			}
			f.Set(reflect.ValueOf(parts))
		default:
			return fmt.Errorf("%s: unsupported field kind %s", key, f.Kind())
		}
	}
	return nil
}

func oneOf(v string, set ...string) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}

// Validate checks all constraints from schemas/config.schema.json that Go's types cannot express.
func (c Config) Validate() error {
	var errs []string
	bad := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	if c.Server.Listen == "" {
		bad("server.listen must not be empty")
	}
	if !oneOf(c.Server.TLS.Mode, "selfsigned", "files", "acme") {
		bad("server.tls.mode must be selfsigned|files|acme, got %q", c.Server.TLS.Mode)
	}
	if c.Server.TLS.Mode == "files" && (c.Server.TLS.CertFile == "" || c.Server.TLS.KeyFile == "") {
		bad("server.tls.mode=files requires certFile and keyFile")
	}
	if !oneOf(c.Server.Exposure, "lan", "remote") {
		bad("server.exposure must be lan|remote, got %q", c.Server.Exposure)
	}
	for _, p := range c.Server.TrustedProxies {
		if !validIPOrCIDR(p) {
			bad("server.trustedProxies entry %q is not an IP or CIDR", p)
		}
	}
	if c.Server.BehindProxy && len(c.Server.TrustedProxies) == 0 {
		bad("server.behindProxy=true requires server.trustedProxies (otherwise client IPs cannot be trusted)")
	}
	if c.Server.TLS.Mode == "acme" {
		bad("server.tls.mode=acme is not implemented yet (planned Phase 1d): use tls.mode=files, or behindProxy=true behind a TLS-terminating proxy/tunnel")
	}
	for _, h := range c.Server.AllowedHosts {
		if !validHostEntry(h) {
			bad("server.allowedHosts entry %q must be a bare hostname or IP (no port, scheme, path or wildcard)", h)
		}
	}
	if c.Addons.BaseDomain != "" && !validHostEntry(c.Addons.BaseDomain) {
		bad("addons.baseDomain %q must be a bare hostname", c.Addons.BaseDomain)
	}
	if !oneOf(c.Auth.Mode, "native", "pam") {
		bad("auth.mode must be native|pam, got %q", c.Auth.Mode)
	}
	if !oneOf(c.Auth.Require2FA, "none", "admins", "all") {
		bad("auth.require2fa must be none|admins|all, got %q", c.Auth.Require2FA)
	}
	if c.Auth.TrustedDeviceDays < 0 || c.Auth.TrustedDeviceDays > 90 {
		bad("auth.trustedDeviceDays must be 0..90")
	}
	if c.Auth.LockoutThreshold < 3 || c.Auth.LockoutThreshold > 100 {
		bad("auth.lockoutThreshold must be 3..100")
	}
	if c.Auth.RemoteIdleMinutes < 5 || c.Auth.RemoteIdleMinutes > c.Auth.SessionIdleMinutes {
		bad("auth.remoteIdleMinutes must be 5..sessionIdleMinutes")
	}
	if c.Auth.AdminRemoteTrustedDeviceDays < 0 || c.Auth.AdminRemoteTrustedDeviceDays > 90 {
		bad("auth.adminRemoteTrustedDeviceDays must be 0..90")
	}
	for _, p := range c.Access.AllowCIDRs {
		if !validIPOrCIDR(p) {
			bad("access.allowCidrs entry %q is not an IP or CIDR", p)
		}
	}
	if c.Auth.LockoutMinutes < 1 || c.Auth.LockoutMinutes > 1440 {
		bad("auth.lockoutMinutes must be 1..1440")
	}
	if c.Server.Exposure == "remote" {
		// Hard safety rules for internet-reachable instances (docs/REMOTE-ACCESS.md §3).
		if len(c.Server.AllowedHosts) == 0 {
			bad("server.exposure=remote requires server.allowedHosts (your public hostname) to defeat DNS rebinding")
		}
		if !c.Server.BehindProxy && c.Server.TLS.Mode != "files" {
			bad("server.exposure=remote requires a trusted certificate: tls.mode=files, or behindProxy=true (built-in ACME is planned)")
		}
		if c.Auth.Mode != "native" {
			bad("server.exposure=remote requires auth.mode=native (PAM mode is LAN-only)")
		}
		if c.Auth.Require2FA != "all" {
			bad("server.exposure=remote requires auth.require2fa=all")
		}
		if c.Auth.AllowRoot {
			bad("server.exposure=remote forbids auth.allowRoot")
		}
	}
	if c.Auth.AdminGroup == "" || c.Auth.UserGroup == "" || c.Auth.AdminGroup == c.Auth.UserGroup {
		bad("auth.adminGroup and auth.userGroup must be set and different")
	}
	if c.Auth.SessionIdleMinutes < 5 || c.Auth.SessionIdleMinutes > 1440 {
		bad("auth.sessionIdleMinutes must be 5..1440")
	}
	if c.Auth.SessionMaxHours < 1 || c.Auth.SessionMaxHours > 168 {
		bad("auth.sessionMaxHours must be 1..168")
	}
	if !oneOf(c.Providers.Storage, "auto", "zfs", "plain", "mock") {
		bad("providers.storage invalid: %q", c.Providers.Storage)
	}
	if !oneOf(c.Providers.GPU, "auto", "nvidia", "amd", "none", "mock") {
		bad("providers.gpu invalid: %q", c.Providers.GPU)
	}
	if !oneOf(c.Providers.Power, "auto", "rapl", "none", "mock") {
		bad("providers.power invalid: %q", c.Providers.Power)
	}
	if len(c.Files.Roots) == 0 {
		bad("files.roots must contain at least one path")
	}
	rootSeen := map[string]bool{}
	for _, r := range c.Files.Roots {
		switch {
		case !filepath.IsAbs(r):
			bad("files.roots entry %q must be absolute", r)
		case filepath.Clean(r) == "/":
			bad("files.roots must not contain \"/\": expose specific directories, not the whole filesystem")
		case rootSeen[filepath.Clean(r)]:
			bad("files.roots entry %q is duplicated", r)
		}
		rootSeen[filepath.Clean(r)] = true
	}
	if c.Terminal.Shell != "" && !filepath.IsAbs(c.Terminal.Shell) {
		bad("terminal.shell must be an absolute path (or empty for the user's login shell)")
	}
	if c.Terminal.MaxSessionsPerUser < 1 || c.Terminal.MaxSessionsPerUser > 50 {
		bad("terminal.maxSessionsPerUser must be 1..50")
	}
	if c.Terminal.IdleTimeoutMinutes < 0 || c.Terminal.DetachedTTLMinutes < 1 {
		bad("terminal.idleTimeoutMinutes must be >= 0 and detachedTtlMinutes >= 1")
	}
	lo, hi, ok := parsePortRange(c.Addons.PortRange)
	if !ok || lo < 1024 || hi > 65535 || lo >= hi {
		bad("addons.portRange must be LOW-HIGH within 1024..65535, got %q", c.Addons.PortRange)
	}
	seen := map[string]bool{}
	for _, cat := range c.Addons.Catalogs {
		if cat.ID == "" || cat.URL == "" || seen[cat.ID] {
			bad("addons.catalogs entries need unique id and url")
		}
		seen[cat.ID] = true
	}
	for _, s := range c.RepoPapers.Sources {
		if !oneOf(s, "github", "arxiv", "semanticscholar", "huggingface", "openalex") {
			bad("repopapers.sources: unknown source %q", s)
		}
	}
	if !oneOf(c.Logging.Level, "debug", "info", "warn", "error") {
		bad("logging.level invalid: %q", c.Logging.Level)
	}
	if !oneOf(c.Logging.Format, "json", "text") {
		bad("logging.format invalid: %q", c.Logging.Format)
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// validHostEntry accepts a bare DNS name or IP literal: no port, scheme, path, wildcard or whitespace.
func validHostEntry(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	if ip := net.ParseIP(strings.Trim(h, "[]")); ip != nil {
		return true
	}
	if strings.ContainsAny(h, ":/\\@*? \t[]") {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

func validIPOrCIDR(s string) bool {
	if _, _, err := net.ParseCIDR(s); err == nil {
		return true
	}
	return net.ParseIP(s) != nil
}

func parsePortRange(s string) (lo, hi int, ok bool) {
	a, b, found := strings.Cut(s, "-")
	if !found {
		return 0, 0, false
	}
	var err1, err2 error
	lo, err1 = strconv.Atoi(a)
	hi, err2 = strconv.Atoi(b)
	return lo, hi, err1 == nil && err2 == nil
}

// Warnings lists NON-fatal problems: settings that validate but have no effect in this build, and risky values.
// The config must never suggest protection that is not actually enforced (docs/ROADMAP.md phases).
func (c Config) Warnings() []string {
	d := Default()
	var w []string
	notEnforced := func(section string, got, def any, phase string) {
		if !equalLoose(reflect.ValueOf(got), reflect.ValueOf(def)) {
			w = append(w, fmt.Sprintf("%s is configured but NOT enforced yet in this build (arrives in %s)", section, phase))
		}
	}
	notEnforced("terminal.*", c.Terminal, d.Terminal, "Phase 3")
	notEnforced("files.*", c.Files, d.Files, "Phase 4")
	notEnforced("providers.*", c.Providers, d.Providers, "Phase 2/5")
	notEnforced("addons.*", c.Addons, d.Addons, "Phase 7; only addons.enabled and addons.baseDomain are used for the add-on list")
	notEnforced("repopapers.*", c.RepoPapers, d.RepoPapers, "Phase 8")
	if c.Auth.Mode == "pam" {
		w = append(w, "auth.mode=pam is not implemented: native credentials are always used")
	}
	if c.Auth.TrustedDeviceDays != d.Auth.TrustedDeviceDays || c.Auth.AdminRemoteTrustedDeviceDays != d.Auth.AdminRemoteTrustedDeviceDays ||
		c.Auth.AllowPasskeyAsSoleFactor != d.Auth.AllowPasskeyAsSoleFactor || c.Auth.BreachCheck {
		w = append(w, "auth.trustedDeviceDays/adminRemoteTrustedDeviceDays/allowPasskeyAsSoleFactor/breachCheck are not used yet (second factors and trusted devices arrive in Phase 1c)")
	}
	if c.Auth.Require2FA != "none" || c.Server.Exposure == "remote" {
		w = append(w, "second-factor verification (TOTP/passkeys) arrives in Phase 1c: until then local users without a second factor get an enrollment-only session, and remote-origin logins are refused")
	}
	for _, p := range c.Server.TrustedProxies {
		if _, n, err := net.ParseCIDR(p); err == nil {
			if ones, bits := n.Mask.Size(); (bits == 32 && ones < 16) || (bits == 128 && ones < 48) {
				w = append(w, fmt.Sprintf("server.trustedProxies entry %q is very broad: any host in it can spoof client IPs; list only your proxy's address", p))
			}
		}
	}
	return w
}

// equalLoose is reflect.DeepEqual where nil and empty slices are equal.
func equalLoose(a, b reflect.Value) bool {
	if a.Kind() != b.Kind() {
		return false
	}
	switch a.Kind() {
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if !equalLoose(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Slice:
		if a.Len() != b.Len() {
			return false
		}
		for i := 0; i < a.Len(); i++ {
			if !equalLoose(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a.Interface(), b.Interface())
	}
}
