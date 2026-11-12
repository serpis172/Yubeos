// Package problem writes RFC 9457 application/problem+json responses.
package problem

import (
	"encoding/json"
	"net/http"
)

// TypeBase prefixes every problem type URI.
const TypeBase = "https://yubeos.dev/problems/"

// FieldError describes one invalid input field (JSON Pointer).
type FieldError struct {
	Pointer string `json:"pointer"`
	Message string `json:"message"`
}

// Problem is the wire format.
type Problem struct {
	Type     string       `json:"type"`
	Title    string       `json:"title"`
	Status   int          `json:"status"`
	Detail   string       `json:"detail,omitempty"`
	Instance string       `json:"instance,omitempty"`
	Errors   []FieldError `json:"errors,omitempty"`
}

// Write sends a problem response. code is the stable machine-readable suffix of Type.
func Write(w http.ResponseWriter, r *http.Request, status int, code, title, detail string, errs ...FieldError) {
	p := Problem{Type: TypeBase + code, Title: title, Status: status, Detail: detail, Errors: errs}
	if r != nil {
		p.Instance = r.URL.Path
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

// Convenience constructors for the common cases listed in docs/API.md.

func Unauthenticated(w http.ResponseWriter, r *http.Request, detail string) {
	Write(w, r, http.StatusUnauthorized, "unauthenticated", "Authentication required", detail)
}

func Forbidden(w http.ResponseWriter, r *http.Request, detail string) {
	Write(w, r, http.StatusForbidden, "forbidden", "Forbidden", detail)
}

func NotFound(w http.ResponseWriter, r *http.Request) {
	Write(w, r, http.StatusNotFound, "not-found", "Not found", "")
}

func Internal(w http.ResponseWriter, r *http.Request) {
	Write(w, r, http.StatusInternalServerError, "internal", "Internal error", "")
}
