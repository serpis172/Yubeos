// Command yubeosctl is the local administration CLI: first admin / reset links, health checks, audit verification.
// It talks to yubeos-priv over the Unix socket as root (docs/AUTHENTICATION.md §9, docs/DEPLOYMENT.md).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/serpis172/Yubeos/internal/audit"
	"github.com/serpis172/Yubeos/internal/config"
	"github.com/serpis172/Yubeos/internal/db"
	"github.com/serpis172/Yubeos/internal/identity"
	"github.com/serpis172/Yubeos/internal/priv"
	"github.com/serpis172/Yubeos/internal/privd/credstore"
	"github.com/serpis172/Yubeos/internal/version"
)

const usage = `yubeosctl — Yubeos administration

Usage:
  yubeosctl user add <linux-user> [--admin] [--ttl MIN]   create the account's one-time setup link (first password)
  yubeosctl user reset <linux-user> [--ttl MIN]           disable the current password and issue a reset link
  yubeosctl user reset <linux-user> --2fa                 remove TOTP, passkeys and trusted devices (sign-in then requires re-enrolment)
  yubeosctl lockdown on|off|status                        block / allow ALL remote-origin requests immediately
  yubeosctl status                                        one-screen summary: version, exposure, lockdown, helper
  yubeosctl doctor [--remote]                             check helper, configuration, file permissions (and remote-access readiness)
  yubeosctl audit verify                                  verify the audit log hash chain
  yubeosctl version

Common flags: --config PATH  --socket PATH  --state-dir PATH  --url https://host
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

type common struct {
	configPath, socket, stateDir, url string
}

func (c *common) flags(fs *flag.FlagSet) {
	fs.StringVar(&c.configPath, "config", "", "config file (default: /etc/yubeos/config.yaml if present)")
	fs.StringVar(&c.socket, "socket", "/run/yubeos/priv.sock", "helper socket")
	fs.StringVar(&c.stateDir, "state-dir", "/var/lib/yubeos", "daemon state directory")
	fs.StringVar(&c.url, "url", "", "public base URL used in the printed link (default: https://<first allowedHosts or hostname>)")
}

func (c *common) loadConfig() (config.Config, error) {
	path := c.configPath
	if path == "" {
		if _, err := os.Stat("/etc/yubeos/config.yaml"); err == nil {
			path = "/etc/yubeos/config.yaml"
		}
	}
	return config.Load(path, os.Environ())
}

func run(args []string, out, errw io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(errw, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintf(out, "yubeosctl %s (%s)\n", version.Version, version.Commit)
		return 0
	case "user":
		return userCmd(args[1:], out, errw)
	case "doctor":
		return doctorCmd(args[1:], out, errw)
	case "audit":
		return auditCmd(args[1:], out, errw)
	case "lockdown":
		return lockdownCmd(args[1:], out, errw)
	case "status":
		return statusCmd(args[1:], out, errw)
	case "help", "-h", "--help":
		fmt.Fprint(out, usage)
		return 0
	}
	fmt.Fprintf(errw, "yubeosctl: unknown command %q\n\n%s", args[0], usage)
	return 2
}

func userCmd(args []string, out, errw io.Writer) int {
	if len(args) < 2 || (args[0] != "add" && args[0] != "reset") {
		fmt.Fprint(errw, usage)
		return 2
	}
	action, name := args[0], strings.ToLower(args[1])
	fs := flag.NewFlagSet("user "+action, flag.ContinueOnError)
	fs.SetOutput(errw)
	var c common
	c.flags(fs)
	admin := fs.Bool("admin", false, "expect the account to be an administrator (checks group membership)")
	ttl := fs.Int("ttl", 60, "link validity in minutes (1..1440)")
	reset2fa := fs.Bool("2fa", false, "reset: remove second factors instead of the password")
	if err := fs.Parse(args[2:]); err != nil {
		return 2
	}
	if *reset2fa {
		if action != "reset" {
			fmt.Fprintln(errw, "yubeosctl: --2fa is only valid with `user reset`")
			return 2
		}
		return reset2FA(&c, name, out, errw)
	}
	if !credstore.ValidUsername.MatchString(name) {
		fmt.Fprintf(errw, "yubeosctl: %q is not a valid Linux account name\n", name)
		return 2
	}
	cfg, err := c.loadConfig()
	if err != nil {
		fmt.Fprintln(errw, "yubeosctl:", err)
		return 1
	}
	if _, err := user.Lookup(name); err != nil {
		fmt.Fprintf(errw, "yubeosctl: no Linux account %q on this machine — Yubeos accounts are linked to Linux accounts (create it with adduser first)\n", name)
		return 1
	}
	info, _ := identity.Linux{AdminGroup: cfg.Auth.AdminGroup, UserGroup: cfg.Auth.UserGroup, AllowRoot: cfg.Auth.AllowRoot}.Lookup(name)
	switch {
	case *admin && info.Role != identity.Admin:
		fmt.Fprintf(errw, "yubeosctl: %s is not in group %q, so it would not be an administrator.\n  Fix: sudo usermod -aG %s %s   (then run this command again)\n", name, cfg.Auth.AdminGroup, cfg.Auth.AdminGroup, name)
		return 1
	case info.Role == "":
		fmt.Fprintf(errw, "warning: %s is in neither %q nor %q: it cannot sign in until you add it to one of them.\n", name, cfg.Auth.AdminGroup, cfg.Auth.UserGroup)
	}
	purpose := "first-password"
	if action == "reset" {
		purpose = "reset"
	}
	var res struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expiresAt"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := priv.NewClient(c.socket).Call(ctx, "auth.setup.create", priv.Meta{Actor: "yubeosctl"}, map[string]any{"username": name, "purpose": purpose, "ttlMinutes": *ttl}, &res); err != nil {
		if errors.Is(err, priv.ErrUnavailable) {
			fmt.Fprintf(errw, "yubeosctl: cannot reach the helper at %s — is yubeos-priv running? (and are you root: sudo yubeosctl …)\n", c.socket)
		} else {
			fmt.Fprintln(errw, "yubeosctl:", err)
		}
		return 1
	}
	base := c.url
	if base == "" {
		host := ""
		if len(cfg.Server.AllowedHosts) > 0 {
			host = cfg.Server.AllowedHosts[0]
		} else if h, err := os.Hostname(); err == nil {
			host = h
		}
		base = "https://" + host
	}
	fmt.Fprintf(out, "Setup link for %s (single use, valid until %s):\n\n  %s/setup?token=%s\n\n", name, time.UnixMilli(res.ExpiresAt).Format(time.RFC1123), strings.TrimRight(base, "/"), res.Token)
	fmt.Fprintln(out, "Open it from a computer on the LOCAL network: setup links are refused from the internet.")
	if action == "reset" {
		fmt.Fprintln(out, "The previous password no longer works and existing sessions of this user end within a minute.")
	}
	return 0
}

type check struct{ level, name, detail string } // level: ok | warn | fail

func doctorCmd(args []string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(errw)
	var c common
	c.flags(fs)
	remote := fs.Bool("remote", false, "also check remote-access readiness (docs/REMOTE-ACCESS.md §7)")
	credDir := fs.String("priv-state-dir", "/var/lib/yubeos-priv", "helper state directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var checks []check
	add := func(level, name, detail string) { checks = append(checks, check{level, name, detail}) }

	cfg, err := c.loadConfig()
	if err != nil {
		add("fail", "configuration", err.Error())
	} else {
		add("ok", "configuration", fmt.Sprintf("valid (exposure=%s, tls=%s)", cfg.Server.Exposure, cfg.Server.TLS.Mode))
		for _, w := range cfg.Warnings() {
			add("warn", "configuration", w)
		}
	}

	var pong struct {
		Version string `json:"version"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := priv.NewClient(c.socket).Call(ctx, "system.ping", priv.Meta{Actor: "yubeosctl"}, nil, &pong); err != nil {
		add("fail", "helper", fmt.Sprintf("cannot reach yubeos-priv at %s (%v)", c.socket, err))
	} else {
		add("ok", "helper", "yubeos-priv "+pong.Version+" answers on "+c.socket)
	}

	permCheck := func(name, path string, wantMax os.FileMode) {
		st, err := os.Stat(path)
		switch {
		case err != nil:
			add("warn", name, path+": "+err.Error())
		case st.Mode().Perm()&^wantMax != 0:
			add("fail", name, fmt.Sprintf("%s has mode %o; must be %o or stricter", path, st.Mode().Perm(), wantMax))
		default:
			add("ok", name, fmt.Sprintf("%s mode %o", path, st.Mode().Perm()))
		}
	}
	permCheck("helper state dir", *credDir, 0o700)
	permCheck("credential store", filepath.Join(*credDir, "credentials.db"), 0o600)
	permCheck("pepper key", filepath.Join(*credDir, "pepper.key"), 0o600)
	permCheck("daemon database", filepath.Join(c.stateDir, "yubeos.db"), 0o600)

	if *remote && err == nil {
		for _, ch := range remoteChecks(cfg, c.stateDir) {
			checks = append(checks, ch)
		}
	}

	code := 0
	for _, ch := range checks {
		mark := map[string]string{"ok": "✔", "warn": "!", "fail": "✘"}[ch.level]
		fmt.Fprintf(out, "[%s] %-18s %s\n", mark, ch.name, ch.detail)
		if ch.level == "fail" {
			code = 1
		}
	}
	return code
}

func auditCmd(args []string, out, errw io.Writer) int {
	if len(args) < 1 || args[0] != "verify" {
		fmt.Fprint(errw, usage)
		return 2
	}
	fs := flag.NewFlagSet("audit verify", flag.ContinueOnError)
	fs.SetOutput(errw)
	var c common
	c.flags(fs)
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	ctx := context.Background()
	d, err := db.OpenReadOnly(ctx, filepath.Join(c.stateDir, "yubeos.db"))
	if err != nil {
		fmt.Fprintln(errw, "yubeosctl:", err)
		return 1
	}
	defer d.Close()
	n, err := audit.New(d, nil, nil).Verify(ctx)
	if err != nil {
		fmt.Fprintf(errw, "audit chain BROKEN after %d valid entries: %v\n", n, err)
		return 1
	}
	fmt.Fprintf(out, "audit chain intact: %d entries verified\n", n)
	return 0
}

// remoteChecks mirrors the readiness list of Settings → Security → Remote access.
func remoteChecks(cfg config.Config, stateDir string) []check {
	lvl := func(ok bool, failLevel string) string {
		if ok {
			return "ok"
		}
		return failLevel
	}
	want := cfg.Server.Exposure == "remote"
	failL := "warn"
	if want {
		failL = "fail"
	}
	var out []check
	out = append(out, check{"ok", "exposure", "server.exposure=" + cfg.Server.Exposure})
	out = append(out,
		check{lvl(len(cfg.Server.AllowedHosts) > 0, failL), "allowedHosts", fmt.Sprintf("%v", cfg.Server.AllowedHosts)},
		check{lvl(cfg.Server.TLS.Mode != "selfsigned" || cfg.Server.BehindProxy, failL), "trusted TLS", "tls.mode=" + cfg.Server.TLS.Mode},
		check{lvl(cfg.Auth.Mode == "native", failL), "auth mode", cfg.Auth.Mode},
		check{lvl(cfg.Auth.Require2FA == "all", failL), "2FA policy", "auth.require2fa=" + cfg.Auth.Require2FA},
		check{lvl(!cfg.Auth.AllowRoot, failL), "root login", fmt.Sprintf("auth.allowRoot=%v", cfg.Auth.AllowRoot)},
	)
	if _, err := os.Stat(filepath.Join(stateDir, "lockdown")); err == nil {
		out = append(out, check{"warn", "lockdown", "ON — remote requests are refused (yubeosctl lockdown off)"})
	} else {
		out = append(out, check{"ok", "lockdown", "off"})
	}
	return out
}

func lockdownCmd(args []string, out, errw io.Writer) int {
	if len(args) < 1 || (args[0] != "on" && args[0] != "off" && args[0] != "status") {
		fmt.Fprint(errw, usage)
		return 2
	}
	fs := flag.NewFlagSet("lockdown", flag.ContinueOnError)
	fs.SetOutput(errw)
	var c common
	c.flags(fs)
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	flagFile := filepath.Join(c.stateDir, "lockdown")
	_, statErr := os.Stat(flagFile)
	switch args[0] {
	case "status":
		if statErr == nil {
			fmt.Fprintln(out, "lockdown: ON (remote requests are refused)")
		} else {
			fmt.Fprintln(out, "lockdown: off")
		}
	case "on":
		if err := os.WriteFile(flagFile, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil { //nolint:gosec // must be readable by the unprivileged daemon
			fmt.Fprintln(errw, "yubeosctl:", err)
			return 1
		}
		fmt.Fprintln(out, "lockdown ON: every request from a remote origin is now refused (local and overlay networks keep working).")
	case "off":
		if err := os.Remove(flagFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(errw, "yubeosctl:", err)
			return 1
		}
		fmt.Fprintln(out, "lockdown off: remote access follows the configuration again.")
	}
	return 0
}

func statusCmd(args []string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(errw)
	var c common
	c.flags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	fmt.Fprintf(out, "yubeosctl %s (%s)\n", version.Version, version.Commit)
	if cfg, err := c.loadConfig(); err != nil {
		fmt.Fprintln(out, "config:   INVALID —", err)
	} else {
		fmt.Fprintf(out, "config:   exposure=%s tls=%s require2fa=%s trustedDeviceDays=%d\n", cfg.Server.Exposure, cfg.Server.TLS.Mode, cfg.Auth.Require2FA, cfg.Auth.TrustedDeviceDays)
	}
	if _, err := os.Stat(filepath.Join(c.stateDir, "lockdown")); err == nil {
		fmt.Fprintln(out, "lockdown: ON")
	} else {
		fmt.Fprintln(out, "lockdown: off")
	}
	var pong struct {
		Version string `json:"version"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := priv.NewClient(c.socket).Call(ctx, "system.ping", priv.Meta{Actor: "yubeosctl"}, nil, &pong); err != nil {
		fmt.Fprintln(out, "helper:   unreachable")
		return 1
	}
	fmt.Fprintln(out, "helper:   yubeos-priv", pong.Version)
	return 0
}

// reset2FA removes TOTP and recovery codes (helper), passkeys and trusted devices (daemon database) and ends the
// user's sessions. Being root on the machine is the root of trust (docs/AUTHENTICATION.md §9).
func reset2FA(c *common, name string, out, errw io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cl := priv.NewClient(c.socket)
	if err := cl.Call(ctx, "auth.totp.remove", priv.Meta{Actor: "yubeosctl"}, map[string]string{"username": name}, nil); err != nil {
		fmt.Fprintln(errw, "yubeosctl:", err)
		return 1
	}
	if err := cl.Call(ctx, "auth.recovery.regenerate", priv.Meta{Actor: "yubeosctl"}, map[string]string{"username": name}, nil); err == nil {
		// regenerate then discard: recovery codes of a reset account must not stay usable
		_ = cl.Call(ctx, "auth.totp.remove", priv.Meta{Actor: "yubeosctl"}, map[string]string{"username": name}, nil)
	}
	d, err := db.Open(ctx, filepath.Join(c.stateDir, "yubeos.db"), db.Migrations(), slog.Default())
	if err != nil {
		fmt.Fprintln(errw, "yubeosctl:", err)
		return 1
	}
	defer d.Close()
	for _, q := range []string{
		`DELETE FROM passkeys WHERE username=?`,
		`UPDATE trusted_devices SET revoked_at=strftime('%s','now')*1000, revoke_reason='admin-2fa-reset' WHERE username=? AND revoked_at IS NULL`,
		`DELETE FROM sessions WHERE username=?`,
		`UPDATE users_meta SET has_2fa=0 WHERE username=?`,
	} {
		if _, err := d.ExecContext(ctx, q, name); err != nil {
			fmt.Fprintln(errw, "yubeosctl:", err)
			return 1
		}
	}
	fmt.Fprintf(out, "Second factors of %s removed; sessions and trusted devices revoked.\nThe user can sign in from the LOCAL network and will be asked to enrol again.\n", name)
	return 0
}
