// Command yubeosd is the Yubeos API daemon: HTTPS REST/WebSocket server with the embedded web UI.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/serpis172/Yubeos/internal/api"
	"github.com/serpis172/Yubeos/internal/audit"
	"github.com/serpis172/Yubeos/internal/auth"
	"github.com/serpis172/Yubeos/internal/clock"
	"github.com/serpis172/Yubeos/internal/config"
	"github.com/serpis172/Yubeos/internal/db"
	"github.com/serpis172/Yubeos/internal/identity"
	"github.com/serpis172/Yubeos/internal/netx"
	"github.com/serpis172/Yubeos/internal/priv"
	"github.com/serpis172/Yubeos/internal/tlsutil"
	"github.com/serpis172/Yubeos/internal/version"
	"github.com/serpis172/Yubeos/web"
)

const demoListen = "127.0.0.1:8443"

func main() { os.Exit(run(os.Args[1:], os.Environ())) }

func run(args []string, environ []string) int {
	fs := flag.NewFlagSet("yubeosd", flag.ContinueOnError)
	var (
		cfgPath    = fs.String("config", "", "path to config file (JSON); empty = defaults + environment")
		demo       = fs.Bool("demo", false, "run with mock hardware and NO authentication; development only (loopback by default)")
		demoPublic = fs.Bool("allow-demo-public", false, "allow --demo to listen on a non-loopback address (dangerous: no authentication)")
		demoSeed   = fs.Int64("demo-seed", 42, "seed for deterministic demo data (used from Phase 2)")
		listen     = fs.String("listen", "", "override server.listen")
		stateDir   = fs.String("state-dir", "", "state directory (default /var/lib/yubeos; demo: user cache dir)")
		privSocket = fs.String("priv-socket", "/run/yubeos/priv.sock", "yubeos-priv socket (not used in --demo)")
		noTLS      = fs.Bool("no-tls", false, "serve plain HTTP (only with --demo, e.g. behind the Vite dev proxy)")
		logLevel   = fs.String("log-level", "", "override logging.level")
		showVer    = fs.Bool("version", false, "print version and exit")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVer {
		fmt.Printf("yubeosd %s (%s)\n", version.Version, version.Commit)
		return 0
	}

	cfg, err := config.Load(*cfgPath, environ)
	if err != nil {
		fmt.Fprintln(os.Stderr, "yubeosd:", err)
		return 2
	}
	if *noTLS && !*demo {
		fmt.Fprintln(os.Stderr, "yubeosd: --no-tls is only allowed together with --demo")
		return 2
	}
	def := config.Default()
	if *demo {
		// Unprivileged-friendly defaults, unless the operator explicitly chose otherwise.
		if cfg.Server.Listen == def.Server.Listen {
			cfg.Server.Listen = demoListen
		}
		if cfg.Server.HTTPRedirect == def.Server.HTTPRedirect {
			cfg.Server.HTTPRedirect = ""
		}
	}
	if *listen != "" {
		cfg.Server.Listen = *listen
	}
	if *logLevel != "" {
		cfg.Logging.Level = *logLevel
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "yubeosd:", err)
		return 2
	}
	if *demo && !*demoPublic && !isLoopbackListen(cfg.Server.Listen) {
		fmt.Fprintf(os.Stderr, "yubeosd: --demo has NO authentication and refuses to listen on %q; use a loopback address (e.g. %s) or pass --allow-demo-public\n",
			cfg.Server.Listen, demoListen)
		return 2
	}

	log := newLogger(cfg.Logging)
	if *demo {
		log.Warn("DEMO MODE: mock hardware, NO AUTHENTICATION — never expose this to a network", "seed", *demoSeed)
	}
	for _, w := range cfg.Warnings() {
		log.Warn(w)
	}

	state := *stateDir
	if state == "" {
		if *demo {
			base, err := os.UserCacheDir()
			if err != nil {
				base = os.TempDir()
			}
			state = filepath.Join(base, "yubeos-demo")
		} else {
			state = "/var/lib/yubeos"
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(ctx, filepath.Join(state, "yubeos.db"), db.Migrations(), log)
	if err != nil {
		log.Error("database", "err", err)
		return 1
	}
	defer database.Close()
	cls, err := newClassifier(cfg)
	if err != nil {
		log.Error("network settings", "err", err)
		return 1
	}
	var authSvc *auth.Service
	if !*demo { // demo mode has its own fake principal and no login
		authSvc = newAuthService(cfg, database, priv.NewClient(*privSocket), log)
		go purgeLoop(ctx, authSvc, log)
	}

	handler := api.New(api.Options{Config: cfg, Log: log, Clock: clock.Real{}, Demo: *demo, UI: web.FS(), Auth: authSvc, Net: cls})
	srv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second, // no Read/WriteTimeout: uploads, WebSockets and terminals are long-lived
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	scheme := "https"
	var certs *tlsutil.Manager
	if *noTLS {
		scheme = "http"
	} else {
		certs, err = newCertManager(cfg.Server, filepath.Join(state, "tls"))
		if err != nil {
			log.Error("tls setup failed", "err", err)
			return 1
		}
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: certs.GetCertificate}
		log.Info("tls certificate loaded", "mode", cfg.Server.TLS.Mode, "sha256", certs.Fingerprint())
		go certs.Run(ctx, 6*time.Hour, log.Warn) // renew self-signed before expiry / pick up replaced files
		go reloadOnSIGHUP(ctx, certs, log)
	}

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		log.Error("listen failed", "addr", srv.Addr, "err", err)
		return 1
	}
	log.Info("yubeosd started", "version", version.Version, "url", scheme+"://"+displayAddr(ln.Addr()), "state", state)

	errc := make(chan error, 1)
	go func() {
		if scheme == "https" {
			errc <- srv.ServeTLS(ln, "", "")
		} else {
			errc <- srv.Serve(ln)
		}
	}()

	// The HTTP→HTTPS redirect is a convenience: if it cannot start (port 80 busy, no CAP_NET_BIND_SERVICE)
	// the HTTPS service keeps running.
	var redirect *http.Server
	if scheme == "https" && cfg.Server.HTTPRedirect != "" {
		redirect = &http.Server{
			Addr:              cfg.Server.HTTPRedirect,
			Handler:           redirectHandler(cfg.Server.Listen, cfg.Server.AllowedHosts),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			log.Info("http→https redirect enabled", "addr", redirect.Addr)
			if err := redirect.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Warn("http→https redirect disabled: cannot listen", "addr", redirect.Addr, "err", err)
			}
		}()
	}

	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "err", err)
			return 1
		}
	}
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if redirect != nil {
		_ = redirect.Shutdown(sctx)
	}
	if err := srv.Shutdown(sctx); err != nil {
		log.Warn("graceful shutdown incomplete", "err", err)
	}
	return 0
}

// newClassifier builds the client-IP/origin classifier from config (trusted proxies, allow-list, overlay policy).
func newClassifier(cfg config.Config) (*netx.Classifier, error) {
	trusted, err := netx.ParseNets(cfg.Server.TrustedProxies)
	if err != nil {
		return nil, err
	}
	allow, err := netx.ParseNets(cfg.Access.AllowCIDRs)
	if err != nil {
		return nil, err
	}
	return &netx.Classifier{Trusted: trusted, Allow: allow, OverlayLocal: cfg.Server.TreatOverlayAsLocal}, nil
}

func newAuthService(cfg config.Config, d *db.DB, p priv.Caller, log *slog.Logger) *auth.Service {
	idle := time.Duration(cfg.Auth.SessionIdleMinutes) * time.Minute
	return auth.NewService(auth.Deps{
		Config: auth.Config{Require2FA: cfg.Auth.Require2FA},
		Priv:   p,
		Identity: &identity.Cached{TTL: 60 * time.Second, Inner: identity.Linux{
			AdminGroup: cfg.Auth.AdminGroup, UserGroup: cfg.Auth.UserGroup, AllowRoot: cfg.Auth.AllowRoot}},
		Sessions: auth.NewSessions(d, clock.Real{}, auth.SessionConfig{
			IdleLocal: idle, IdleRemote: time.Duration(cfg.Auth.RemoteIdleMinutes) * time.Minute,
			MaxAge: time.Duration(cfg.Auth.SessionMaxHours) * time.Hour}),
		Throttle: auth.NewThrottle(d, clock.Real{}, auth.ThrottleConfig{
			LockoutThreshold: cfg.Auth.LockoutThreshold, LockoutDuration: time.Duration(cfg.Auth.LockoutMinutes) * time.Minute}),
		Audit: audit.New(d, clock.Real{}, log), Clock: clock.Real{}, Log: log,
	})
}

// purgeLoop removes expired sessions and old throttling records every 10 minutes.
func purgeLoop(ctx context.Context, s *auth.Service, log *slog.Logger) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.Purge(ctx); err != nil {
				log.Warn("purge failed", "err", err)
			}
		}
	}
}

func newLogger(c config.Logging) *slog.Logger {
	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(c.Level))
	opts := &slog.HandlerOptions{Level: lvl}
	if c.Format == "text" {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}

// newCertManager selects the certificate source from config. "acme" is rejected by config validation until implemented.
func newCertManager(s config.Server, dir string) (*tlsutil.Manager, error) {
	switch s.TLS.Mode {
	case "files":
		return tlsutil.NewManager(tlsutil.FileLoader(s.TLS.CertFile, s.TLS.KeyFile))
	case "selfsigned":
		return tlsutil.NewManager(tlsutil.SelfSignedLoader(dir, s.AllowedHosts))
	default:
		return nil, fmt.Errorf("tls mode %q is not implemented yet", s.TLS.Mode)
	}
}

func reloadOnSIGHUP(ctx context.Context, m *tlsutil.Manager, log *slog.Logger) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	defer signal.Stop(ch)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			switch changed, err := m.Reload(); {
			case err != nil:
				log.Warn("SIGHUP: certificate reload failed", "err", err)
			case changed:
				log.Info("SIGHUP: certificate reloaded", "sha256", m.Fingerprint())
			default:
				log.Info("SIGHUP: certificate unchanged")
			}
		}
	}
}

// redirectHandler answers plain HTTP with a temporary redirect to the HTTPS listener. When allowedHosts is set,
// other Host headers are refused (no open redirect through a forged Host).
func redirectHandler(httpsListen string, allowedHosts []string) http.Handler {
	_, port, _ := net.SplitHostPort(httpsListen)
	allowed := make(map[string]bool, len(allowedHosts))
	for _, h := range allowedHosts {
		allowed[strings.ToLower(strings.Trim(h, "[]"))] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if hh, _, err := net.SplitHostPort(host); err == nil {
			host = hh
		}
		host = strings.Trim(strings.ToLower(host), "[]")
		if len(allowed) > 0 && !allowed[host] {
			http.Error(w, "host not allowed", http.StatusForbidden)
			return
		}
		target := "https://" + hostForURL(host)
		if port != "" && port != "443" {
			target += ":" + port
		}
		http.Redirect(w, r, target+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	})
}

func hostForURL(h string) string {
	if strings.Contains(h, ":") { // IPv6 literal
		return "[" + h + "]"
	}
	return h
}

// isLoopbackListen reports whether a listen address binds only to loopback (an empty host means all interfaces).
func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func displayAddr(a net.Addr) string {
	s := a.String()
	for _, p := range []string{"[::]:", "0.0.0.0:"} {
		if strings.HasPrefix(s, p) {
			return "localhost:" + strings.TrimPrefix(s, p)
		}
	}
	return s
}
