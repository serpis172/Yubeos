package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/serpis172/Yubeos/internal/problem"
)

const maxPrefsBytes = 128 << 10

// PrefsStore keeps the per-user interface preferences as one JSON document.
type PrefsStore interface {
	Get(ctx context.Context, user string) (raw []byte, found bool, err error)
	Put(ctx context.Context, user string, raw []byte, at time.Time) error
}

// SQLPrefs stores preferences in the settings table, key "prefs:<username>".
type SQLPrefs struct{ DB *sql.DB }

func (s SQLPrefs) Get(ctx context.Context, user string) ([]byte, bool, error) {
	var v string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, "prefs:"+user).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return []byte(v), err == nil, err
}

func (s SQLPrefs) Put(ctx context.Context, user string, raw []byte, at time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO settings(key,value,updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		"prefs:"+user, string(raw), at.Unix())
	return err
}

// MemPrefs is used in demo mode and whenever no database store is configured.
type MemPrefs struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *MemPrefs) Get(_ context.Context, user string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[user]
	return v, ok, nil
}

func (s *MemPrefs) Put(_ context.Context, user string, raw []byte, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string][]byte{}
	}
	s.m[user] = raw
	return nil
}

type preferences struct {
	Theme        string           `json:"theme"`
	Language     string           `json:"language"`
	Units        string           `json:"units"`
	Temperature  string           `json:"temperature"`
	CustomThemes []map[string]any `json:"customThemes"`
	UpdatedAt    int64            `json:"updatedAt"`
}

func defaultPrefs() preferences {
	return preferences{Theme: "system", Language: "en", Units: "bytes", Temperature: "c", CustomThemes: []map[string]any{}}
}

var themeID = regexp.MustCompile(`^(system|[a-z0-9-]{1,40}|custom:[A-Za-z0-9_-]{1,40})$`)

func (p preferences) validate() []problem.FieldError {
	var errs []problem.FieldError
	bad := func(field, msg string) { errs = append(errs, problem.FieldError{Pointer: "/" + field, Message: msg}) }
	if !themeID.MatchString(p.Theme) {
		bad("theme", "must be system, a preset id or custom:<id>")
	}
	if p.Language != "en" && p.Language != "it" {
		bad("language", "must be en or it")
	}
	if p.Units != "bytes" && p.Units != "bits" {
		bad("units", "must be bytes or bits")
	}
	if p.Temperature != "c" && p.Temperature != "f" {
		bad("temperature", "must be c or f")
	}
	if len(p.CustomThemes) > 20 {
		bad("customThemes", "at most 20 custom themes")
	}
	return errs
}

func (s *Server) prefsStore() PrefsStore { return s.prefs }

func (s *Server) getPrefs(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	out := defaultPrefs()
	raw, found, err := s.prefsStore().Get(r.Context(), p.Username)
	if err != nil {
		s.opts.Log.Error("preferences read", "err", err)
		problem.Write(w, r, http.StatusInternalServerError, "internal", "Could not read preferences", "")
		return
	}
	if found {
		if err := json.Unmarshal(raw, &out); err != nil { // corrupt row: fall back to defaults rather than lock the UI
			s.opts.Log.Warn("preferences unreadable, using defaults", "err", err)
			out = defaultPrefs()
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putPrefs(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxPrefsBytes)
	in := defaultPrefs()
	in.CustomThemes = nil
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.CustomThemes == nil {
		in.CustomThemes = []map[string]any{}
	}
	if errs := in.validate(); len(errs) > 0 {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation", "Invalid preferences", "", errs...)
		return
	}
	now := s.opts.Clock.Now()
	in.UpdatedAt = now.UnixMilli()
	raw, _ := json.Marshal(in)
	if len(raw) > maxPrefsBytes {
		problem.Write(w, r, http.StatusRequestEntityTooLarge, "payload-too-large", "Preferences too large", "")
		return
	}
	if err := s.prefsStore().Put(r.Context(), p.Username, raw, now); err != nil {
		s.opts.Log.Error("preferences write", "err", err)
		problem.Write(w, r, http.StatusInternalServerError, "internal", "Could not save preferences", "")
		return
	}
	writeJSON(w, http.StatusOK, in)
}
