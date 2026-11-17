package api

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"

	"github.com/serpis172/Yubeos/internal/netx"
	"github.com/serpis172/Yubeos/internal/problem"
)

// hsts adds Strict-Transport-Security; enabled only when exposure=remote (docs/REMOTE-ACCESS.md §3).
func hsts(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !enabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			next.ServeHTTP(w, r)
		})
	}
}

// Body limits for state-changing API calls (docs/AUTHENTICATION.md §8). Streaming endpoints (file upload,
// terminal) register their own limits and are listed in bodyLimitExempt.
const (
	defaultBodyLimit  = 1 << 20  // 1 MiB
	authBodyLimit     = 4 << 10  // 4 KiB
	webauthnBodyLimit = 16 << 10 // WebAuthn attestations are larger
)

var bodyLimitExempt = []string{"/api/v1/files/upload"}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead {
			limit := int64(defaultBodyLimit)
			switch {
			case hasAnyPrefix(r.URL.Path, bodyLimitExempt):
				limit = -1
			case strings.HasPrefix(r.URL.Path, "/api/v1/auth/webauthn/"):
				limit = webauthnBodyLimit
			case strings.HasPrefix(r.URL.Path, "/api/v1/auth/"):
				limit = authBodyLimit
			}
			if limit > 0 {
				if r.ContentLength > limit {
					problem.Write(w, r, http.StatusRequestEntityTooLarge, "payload-too-large", "Request body too large", "")
					return
				}
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// gzipMW compresses JSON/text/JS/SVG responses >= 1 KiB for clients that accept gzip. It never touches
// WebSocket upgrades, Range requests or HEAD, and weakens a strong ETag when it changes the representation.
func gzipMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || r.Method == http.MethodHead ||
			r.Header.Get("Range") != "" || r.Header.Get("Upgrade") != "" {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

type gzipWriter struct {
	http.ResponseWriter
	zw          *gzip.Writer
	wroteHeader bool
}

func compressible(ct string) bool {
	ct = strings.ToLower(ct)
	for _, p := range []string{"text/", "application/json", "application/problem+json", "application/javascript", "text/javascript", "image/svg+xml"} {
		if strings.HasPrefix(ct, p) {
			return true
		}
	}
	return false
}

func (g *gzipWriter) WriteHeader(code int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	h := g.Header()
	ok := code >= 200 && code != http.StatusNoContent && code != http.StatusNotModified &&
		h.Get("Content-Encoding") == "" && compressible(h.Get("Content-Type"))
	if ok {
		if cl := h.Get("Content-Length"); cl != "" {
			if n, err := strconv.Atoi(cl); err == nil && n < 1024 {
				ok = false
			}
		}
	}
	if ok {
		h.Del("Content-Length")
		h.Set("Content-Encoding", "gzip")
		h.Add("Vary", "Accept-Encoding")
		if et := h.Get("ETag"); et != "" && !strings.HasPrefix(et, "W/") {
			h.Set("ETag", "W/"+et)
		}
		g.zw = gzip.NewWriter(g.ResponseWriter)
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(p []byte) (int, error) {
	if !g.wroteHeader {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(p))
		}
		g.WriteHeader(http.StatusOK)
	}
	if g.zw != nil {
		return g.zw.Write(p)
	}
	return g.ResponseWriter.Write(p)
}

func (g *gzipWriter) close() {
	if g.zw != nil {
		_ = g.zw.Close()
	}
}

func (g *gzipWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

// accessGuard enforces access.allowCidrs for REMOTE origins before any other logic runs (local origins always pass).
func accessGuard(c *netx.Classifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(c.Allow) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !c.Permitted(c.ClientIP(r)) {
				problem.Write(w, r, http.StatusForbidden, "access-denied", "Access denied", "")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
