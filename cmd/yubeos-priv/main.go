// Command yubeos-priv is the root helper: a typed, allow-listed RPC server on a Unix socket that owns
// the credential store (docs/SECURITY.md §4, docs/AUTHENTICATION.md §3). It has no network access and no
// "run this command" method.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/serpis172/Yubeos/internal/privd"
	"github.com/serpis172/Yubeos/internal/privd/credstore"
	"github.com/serpis172/Yubeos/internal/version"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("yubeos-priv", flag.ContinueOnError)
	var (
		socket     = fs.String("socket", "/run/yubeos/priv.sock", "Unix socket path")
		stateDir   = fs.String("state-dir", "/var/lib/yubeos-priv", "credential store directory (0700)")
		daemonUser = fs.String("daemon-user", "yubeos", "Linux user the yubeosd daemon runs as (allowed peer)")
		extraUIDs  = fs.String("allow-uid", "", "additional peer uids, comma-separated (tests/development only)")
		group      = fs.String("socket-group", "", "group that owns the socket (default: the daemon user's primary group)")
		logLevel   = fs.String("log-level", "info", "debug|info|warn|error")
		showVer    = fs.Bool("version", false, "print version and exit")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVer {
		fmt.Printf("yubeos-priv %s (%s)\n", version.Version, version.Commit)
		return 0
	}
	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(*logLevel))
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))

	allowed := []uint32{0} // root: yubeosctl
	sockGroup := *group
	if u, err := user.Lookup(*daemonUser); err == nil {
		if id, err := strconv.ParseUint(u.Uid, 10, 32); err == nil {
			allowed = append(allowed, uint32(id))
		}
		if sockGroup == "" {
			if g, err := user.LookupGroupId(u.Gid); err == nil {
				sockGroup = g.Name
			}
		}
	} else if *extraUIDs == "" {
		log.Error("daemon user not found; create it or pass --allow-uid", "user", *daemonUser)
		return 1
	}
	for _, s := range strings.Split(*extraUIDs, ",") {
		if s = strings.TrimSpace(s); s != "" {
			id, err := strconv.ParseUint(s, 10, 32)
			if err != nil {
				fmt.Fprintln(os.Stderr, "yubeos-priv: invalid --allow-uid:", s)
				return 2
			}
			allowed = append(allowed, uint32(id))
		}
	}
	if os.Geteuid() != 0 {
		log.Warn("not running as root: development mode, privileged features will not work")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	syscall.Umask(0o077)
	store, err := credstore.Open(ctx, *stateDir, credstore.Options{Log: log})
	if err != nil {
		log.Error("credential store", "err", err)
		return 1
	}
	defer store.Close()

	srv := privd.NewServer(log, allowed...)
	privd.RegisterSystem(srv)
	privd.RegisterCompose(srv) // read-only add-on discovery
	credstore.Register(srv, store)

	if err := os.MkdirAll(dirOf(*socket), 0o750); err != nil {
		log.Error("socket dir", "err", err)
		return 1
	}
	ln, err := privd.ListenUnix(*socket, sockGroup)
	if err != nil {
		log.Error("listen", "err", err)
		return 1
	}
	log.Info("yubeos-priv started", "version", version.Version, "socket", *socket, "allowedUIDs", allowed, "methods", len(srv.Methods()))
	if err := srv.Serve(ctx, ln); err != nil {
		log.Error("serve", "err", err)
		return 1
	}
	_ = os.Remove(*socket)
	log.Info("yubeos-priv stopped")
	_ = time.Now
	return 0
}

func dirOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i > 0 {
		return p[:i]
	}
	return "."
}
