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
	"github.com/serpis172/Yubeos/internal/clock"
	"github.com/serpis172/Yubeos/internal/config"
	"github.com/serpis172/Yubeos/internal/tlsutil"
	"github.com/serpis172/Yubeos/internal/version"
	"github.com/serpis172/Yubeos/web"
)

func main() { os.Exit(run(os.Args[1:], os.Environ())) }

func run(args []string, environ []string) int {
	fs := flag.NewFlagSet("yubeosd", flag.ContinueOnError)
	var (
		cfgPath  = fs.String("config", "", "path to config file (JSON); empty = defaults + environment")
		demo     = fs.Bool("demo", false, "run with mock hardware and no authentication; for development only")
		demoSeed = fs.Int64("demo-seed", 42, "seed for deterministic demo data (used from Phase 2)")
		listen   = fs.String("listen", "", "override server.listen")
		stateDir = fs.String("state-dir", "", "state directory (default /var/lib/yubeos; demo: user cache dir)")
		noTLS    = fs.Bool("no-tls", false, "serve plain HTTP (only allowed with --demo, e.g. behind the Vite dev proxy)")
		logLevel = fs.String("log-level", "", "override logging.level")
		showVer  = fs.Bool("version", false, "print version and exit")
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
	def := config.Default()
	if *demo {
		// Unprivileged-friendly defaults, unless the operator explicitly chose otherwise.
		if cfg.Server.Listen == def.Server.Listen {
			cfg.Server.Listen = ":8443"
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
	if *noTLS && !*demo {
		fmt.Fprintln(os.Stderr, "yubeosd: --no-tls is only allowed together with --demo")
		return 2
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "yubeosd:", err)
		return 2
	}

	log := newLogger(cfg.Logging)
	if *demo {
		log.Warn("DEMO MODE: mock hardware, NO AUTHENTICATION — never expose this to a network", "seed", *demoSeed)
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

	handler := api.New(api.Options{Config: cfg, Log: log, Clock: clock.Real{}, Demo: *demo, UI: web.FS()})
	srv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second, // no Read/WriteTimeout: uploads, WebSockets and terminals are long-lived
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	scheme := "https"
	if *noTLS {
		scheme = "http"
	} else {
		tc, fp, err := tlsConfig(cfg.Server, filepath.Join(state, "tls"))
		if err != nil {
			log.Error("tls setup failed", "err", err)
			return 1
		}
		srv.TLSConfig = tc
		if fp != "" {
			log.Info("self-signed certificate in use", "sha256", fp)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		log.Error("listen failed", "addr", srv.Addr, "err", err)
		return 1
	}
	log.Info("yubeosd started", "version", version.Version, "url", scheme+"://"+displayAddr(ln.Addr()), "state", state)

	errc := make(chan error, 2)
	go func() {
		if scheme == "https" {
			errc <- srv.ServeTLS(ln, "", "")
		} else {
			errc <- srv.Serve(ln)
		}
	}()
	if scheme == "https" && cfg.Server.HTTPRedirect != "" {
		go func() { errc <- serveRedirect(cfg.Server.HTTPRedirect, cfg.Server.Listen, log) }()
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
	if err := srv.Shutdown(sctx); err != nil {
		log.Warn("graceful shutdown incomplete", "err", err)
	}
	return 0
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

func tlsConfig(s config.Server, dir string) (*tls.Config, string, error) {
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	switch s.TLS.Mode {
	case "files":
		c, err := tls.LoadX509KeyPair(s.TLS.CertFile, s.TLS.KeyFile)
		if err != nil {
			return nil, "", fmt.Errorf("load certificate files: %w", err)
		}
		tc.Certificates = []tls.Certificate{c}
		return tc, "", nil
	case "selfsigned":
		c, fp, err := tlsutil.LoadOrCreate(dir, s.AllowedHosts, time.Now())
		if err != nil {
			return nil, "", err
		}
		tc.Certificates = []tls.Certificate{c}
		return tc, fp, nil
	default:
		return nil, "", fmt.Errorf("tls mode %q is not implemented yet", s.TLS.Mode)
	}
}

// serveRedirect answers plain HTTP with a redirect to the HTTPS listener.
func serveRedirect(addr, httpsListen string, log *slog.Logger) error {
	_, port, _ := net.SplitHostPort(httpsListen)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if hh, _, err := net.SplitHostPort(host); err == nil {
			host = hh
		}
		target := "https://" + host
		if port != "" && port != "443" {
			target += ":" + port
		}
		http.Redirect(w, r, target+r.URL.RequestURI(), http.StatusPermanentRedirect)
	})
	s := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	log.Info("http→https redirect enabled", "addr", addr)
	return s.ListenAndServe()
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
