package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/serpis172/Yubeos/internal/metrics"
	"github.com/serpis172/Yubeos/internal/netx"
	"github.com/serpis172/Yubeos/internal/problem"
)

const (
	maxSeriesPerRequest = 64
	maxWSPerUser        = 8
	maxWSTotal          = 64
	wsWriteTimeout      = 5 * time.Second
	wsRevalidate        = 30 * time.Second
)

func (s *Server) registerMetricsRoutes(mux *http.ServeMux) {
	if s.opts.Metrics == nil {
		return
	}
	mux.Handle("GET /api/v1/metrics/catalog", s.require(RoleUser, s.metricsCatalog))
	mux.Handle("GET /api/v1/metrics/snapshot", s.require(RoleUser, s.metricsSnapshot))
	mux.Handle("GET /api/v1/metrics/history", s.require(RoleUser, s.metricsHistory))
	mux.Handle("GET /ws/metrics", s.require(RoleUser, s.metricsWS))
}

func (s *Server) metricsCatalog(w http.ResponseWriter, _ *http.Request) {
	series, devices, providers := s.opts.Metrics.Catalog()
	if devices == nil {
		devices = []metrics.Device{}
	}
	if providers == nil {
		providers = []metrics.ProviderStatus{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"series": series, "devices": devices, "providers": providers})
}

func (s *Server) metricsSnapshot(w http.ResponseWriter, _ *http.Request) {
	t, values, stale := s.opts.Metrics.Snapshot()
	if stale == nil {
		stale = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"t": t, "values": values, "stale": stale})
}

// historyRanges: range → (duration, default step). Ranges up to one hour come from the ring buffer, longer ones from
// the minute rollups.
var historyRanges = map[string]struct{ d, step time.Duration }{
	"5m":  {5 * time.Minute, 0},
	"1h":  {time.Hour, 5 * time.Second},
	"24h": {24 * time.Hour, time.Minute},
	"7d":  {7 * 24 * time.Hour, 10 * time.Minute},
	"30d": {30 * 24 * time.Hour, 30 * time.Minute},
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s *Server) metricsHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rg, ok := historyRanges[q.Get("range")]
	if !ok {
		problem.Write(w, r, http.StatusBadRequest, "validation", "Invalid range", "range must be 5m, 1h, 24h, 7d or 30d")
		return
	}
	patterns := splitList(q.Get("series"))
	if len(patterns) == 0 || len(patterns) > maxSeriesPerRequest {
		problem.Write(w, r, http.StatusBadRequest, "validation", "Invalid series", "give 1 to "+strconv.Itoa(maxSeriesPerRequest)+" series or prefix.* patterns")
		return
	}
	step := rg.step
	if v := q.Get("step"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 86400 {
			problem.Write(w, r, http.StatusBadRequest, "validation", "Invalid step", "step is a number of seconds, 0..86400")
			return
		}
		step = time.Duration(n) * time.Second
	}
	keys := s.opts.Metrics.Keys(patterns)
	if len(keys) > 256 {
		keys = keys[:256]
	}
	var data map[string]metrics.Points
	if rg.d <= time.Hour || s.opts.Rollup == nil {
		data = s.opts.Metrics.History(keys, rg.d, step)
	} else {
		now := s.opts.Clock.Now()
		var err error
		if data, err = s.opts.Rollup.History(r.Context(), keys, now.Add(-rg.d), now, step); err != nil {
			s.opts.Log.Error("metrics history", "err", err)
			problem.Internal(w, r)
			return
		}
	}
	if data == nil {
		data = map[string]metrics.Points{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"range": q.Get("range"), "stepSeconds": int(step / time.Second), "series": data})
}

// wsLimiter bounds concurrent WebSocket connections per user and in total.
type wsLimiter struct {
	mu    sync.Mutex
	user  map[string]int
	total int
}

func (l *wsLimiter) acquire(user string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.user == nil {
		l.user = map[string]int{}
	}
	if l.total >= maxWSTotal || l.user[user] >= maxWSPerUser {
		return false
	}
	l.user[user]++
	l.total++
	return true
}

func (l *wsLimiter) release(user string) {
	l.mu.Lock()
	l.user[user]--
	l.total--
	l.mu.Unlock()
}

func (s *Server) wsOriginPatterns() []string {
	if s.opts.Demo {
		return []string{"localhost:*", "127.0.0.1:*", "[::1]:*"}
	}
	return append([]string(nil), s.opts.Config.Server.AllowedHosts...) // same host is always accepted
}

// metricsWS streams frames {t, v:{key:number}} for the requested series (exact keys or prefix.* patterns), an
// immediate snapshot first, and {dropped:n} when this client was too slow. The Origin header must match the host.
func (s *Server) metricsWS(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	patterns := splitList(r.URL.Query().Get("series"))
	if len(patterns) > maxSeriesPerRequest {
		problem.Write(w, r, http.StatusBadRequest, "validation", "Too many series", "")
		return
	}
	if !s.ws.acquire(p.Username) {
		problem.Write(w, r, http.StatusTooManyRequests, "too-many-connections", "Too many live connections", "close other Yubeos tabs and retry")
		return
	}
	defer s.ws.release(p.Username)

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.wsOriginPatterns()})
	if err != nil {
		return // Accept already answered (bad origin, not an upgrade request, …)
	}
	defer conn.CloseNow()
	ctx := conn.CloseRead(r.Context())

	sub := s.opts.Metrics.Subscribe()
	defer sub.Close()
	send := func(v any) bool {
		wctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
		defer cancel()
		b, _ := json.Marshal(v)
		return conn.Write(wctx, websocket.MessageText, b) == nil
	}

	t, values, _ := s.opts.Metrics.Snapshot()
	first := metrics.Frame{T: t, V: map[string]float64{}}
	for k, v := range values {
		if metrics.Match(patterns, k) {
			first.V[k] = v
		}
	}
	if !send(first) {
		return
	}

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	check := time.NewTicker(wsRevalidate)
	defer check.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-sub.C:
			if n := sub.Dropped(); n > 0 && !send(map[string]int{"dropped": n}) {
				return
			}
			out := metrics.Frame{T: f.T, V: map[string]float64{}}
			for k, v := range f.V {
				if metrics.Match(patterns, k) {
					out.V[k] = v
				}
			}
			if len(out.V) > 0 && !send(out) {
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		case <-check.C:
			if !s.wsStillAllowed(ctx, r, p) {
				_ = conn.Close(websocket.StatusPolicyViolation, "session ended")
				return
			}
		}
	}
}

// wsStillAllowed re-checks, every 30 s, that the session is alive and that lockdown has not been switched on.
func (s *Server) wsStillAllowed(ctx context.Context, r *http.Request, p Principal) bool {
	if p.Session != nil && p.Session.Origin == netx.Remote && s.ld.Active() {
		return false
	}
	if s.opts.Auth == nil || p.Session == nil {
		return true // demo
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	_, err = s.opts.Auth.Authenticate(ctx, c.Value)
	return err == nil
}
