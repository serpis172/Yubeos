package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/OWNER/yubeos/internal/problem"
)

type ctxKey int

const (
	keyReqID ctxKey = iota
	keyPrincipal
)

// RequestID returns the request ID stored in ctx ("" if none).
func RequestID(ctx context.Context) string {
	s, _ := ctx.Value(keyReqID).(string)
	return s
}

// chain applies middleware so that the first argument is the outermost.
func chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b [8]byte
		_, _ = rand.Read(b[:])
		id := hex.EncodeToString(b[:]) // never trust a client-supplied ID
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyReqID, id)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(p []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(p)
	s.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach Flusher/Hijacker (needed for WebSockets later).
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func accessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			lvl := slog.LevelInfo
			if strings.HasPrefix(r.URL.Path, "/assets/") || r.URL.Path == "/healthz" {
				lvl = slog.LevelDebug // keep the default log quiet
			}
			log.Log(r.Context(), lvl, "http",
				"reqId", RequestID(r.Context()), "method", r.Method, "path", r.URL.Path,
				"status", rec.status, "bytes", rec.bytes, "ms", time.Since(start).Milliseconds())
		})
	}
}

func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					log.Error("panic", "reqId", RequestID(r.Context()), "value", v, "stack", string(debug.Stack()))
					problem.Internal(w, r)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// csp mirrors docs/SECURITY.md §8. style-src 'unsafe-inline' is tracked as TD-001.
const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"font-src 'self'; connect-src 'self' wss:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// hostGuard defends against DNS rebinding: when allowed is non-empty, the Host header must match.
func hostGuard(allowed []string) func(http.Handler) http.Handler {
	set := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		set[strings.ToLower(a)] = true
	}
	return func(next http.Handler) http.Handler {
		if len(set) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			host = strings.Trim(strings.ToLower(host), "[]")
			if !set[host] {
				problem.Write(w, r, http.StatusForbidden, "host-not-allowed", "Host not allowed",
					"add this host to server.allowedHosts")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
