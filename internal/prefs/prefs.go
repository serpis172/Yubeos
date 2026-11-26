// Package prefs stores per-user interface preferences (docs/modules/appearance.md). The server validates what it
// stores so a bad client cannot park arbitrary data in the database; the UI validates again when it reads.
package prefs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"
)

const (
	MaxBytes        = 128 << 10
	maxThemes       = 12
	maxThemeBytes   = 16 << 10
	maxThemeNameLen = 60
)

// Prefs is the wire and storage format.
type Prefs struct {
	Theme        string            `json:"theme"`        // system | preset id | custom:<id>
	Language     string            `json:"language"`     // en | it
	Units        string            `json:"units"`        // bytes | bits  (network rates)
	Temperature  string            `json:"temperature"`  // c | f
	CustomThemes []json.RawMessage `json:"customThemes"` // each validated like schemas/theme.schema.json
	UpdatedAt    int64             `json:"updatedAt"`    // unix ms; 0 = never saved
}

// Defaults is what a user with nothing saved gets.
func Defaults() Prefs {
	return Prefs{Theme: "system", Language: "en", Units: "bytes", Temperature: "c", CustomThemes: []json.RawMessage{}}
}

var (
	themeChoice = regexp.MustCompile(`^(system|[a-z0-9][a-z0-9-]{1,47}|custom:[a-z0-9][a-z0-9-]{1,47})$`)
	themeID     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,47}$`)
	hexColor    = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	fontList    = regexp.MustCompile(`^[A-Za-z0-9 ,'"_.-]{1,120}$`)
)

// ErrInvalid wraps every validation failure (maps to HTTP 422).
var ErrInvalid = errors.New("prefs: invalid")

func bad(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalid}, a...)...)
}

// Validate checks p and fills empty enums with defaults.
func Validate(p *Prefs) error {
	d := Defaults()
	if p.Theme == "" {
		p.Theme = d.Theme
	}
	if p.Language == "" {
		p.Language = d.Language
	}
	if p.Units == "" {
		p.Units = d.Units
	}
	if p.Temperature == "" {
		p.Temperature = d.Temperature
	}
	if p.CustomThemes == nil {
		p.CustomThemes = []json.RawMessage{}
	}
	if !themeChoice.MatchString(p.Theme) {
		return bad("theme")
	}
	if p.Language != "en" && p.Language != "it" {
		return bad("language must be en or it")
	}
	if p.Units != "bytes" && p.Units != "bits" {
		return bad("units must be bytes or bits")
	}
	if p.Temperature != "c" && p.Temperature != "f" {
		return bad("temperature must be c or f")
	}
	if len(p.CustomThemes) > maxThemes {
		return bad("at most %d custom themes", maxThemes)
	}
	seen := map[string]bool{}
	for i, raw := range p.CustomThemes {
		id, err := validateTheme(raw)
		if err != nil {
			return bad("customThemes[%d]: %v", i, err)
		}
		if seen[id] {
			return bad("customThemes[%d]: duplicate id", i)
		}
		seen[id] = true
	}
	return nil
}

func validateTheme(raw json.RawMessage) (string, error) {
	if len(raw) > maxThemeBytes {
		return "", errors.New("theme too large")
	}
	// Strict top-level shape: reject unknown keys with a generic map pass, then decode typed.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return "", errors.New("not an object")
	}
	for k := range keys {
		switch k {
		case "schemaVersion", "id", "name", "author", "base", "colors", "typography", "shape", "layout", "motion", "background":
		default:
			return "", fmt.Errorf("unknown property %q", k)
		}
	}
	var t struct {
		SchemaVersion int            `json:"schemaVersion"`
		ID            string         `json:"id"`
		Name          string         `json:"name"`
		Author        string         `json:"author"`
		Base          string         `json:"base"`
		Colors        map[string]any `json:"colors"`
		Typography    map[string]any `json:"typography"`
		Shape         map[string]any `json:"shape"`
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return "", errors.New("wrong field types")
	}
	if t.SchemaVersion != 1 || !themeID.MatchString(t.ID) || t.Name == "" || len([]rune(t.Name)) > maxThemeNameLen || len([]rune(t.Author)) > 80 {
		return "", errors.New("schemaVersion/id/name/author")
	}
	if t.Base != "dark" && t.Base != "light" {
		return "", errors.New("base")
	}
	for _, k := range []string{"bg", "surface1", "surface2", "border", "text", "textMuted", "accent", "accentContrast", "success", "warning", "danger", "info"} {
		if s, ok := t.Colors[k].(string); !ok || !hexColor.MatchString(s) {
			return "", fmt.Errorf("colors.%s", k)
		}
	}
	chart, ok := t.Colors["chart"].([]any)
	if !ok || len(chart) != 8 {
		return "", errors.New("colors.chart needs 8 colours")
	}
	for _, c := range chart {
		if s, ok := c.(string); !ok || !hexColor.MatchString(s) {
			return "", errors.New("colors.chart")
		}
	}
	for k := range t.Colors {
		if k != "chart" && k != "terminal" && !hexColor.MatchString(fmt.Sprint(t.Colors[k])) {
			return "", fmt.Errorf("colors.%s", k)
		}
	}
	for _, k := range []string{"ui", "mono", "display"} {
		v, present := t.Typography[k]
		if !present && k == "display" {
			continue
		}
		if s, ok := v.(string); !ok || !fontList.MatchString(s) {
			return "", fmt.Errorf("typography.%s", k)
		}
	}
	inRange := func(m map[string]any, k string, lo, hi float64) bool {
		v, present := m[k]
		if !present {
			return true
		}
		f, ok := v.(float64)
		return ok && f >= lo && f <= hi
	}
	if !inRange(t.Typography, "baseSizePx", 12, 18) || !inRange(t.Typography, "lineHeight", 1.2, 2) || !inRange(t.Typography, "headingWeight", 400, 800) || !inRange(t.Shape, "radiusPx", 0, 16) {
		return "", errors.New("numeric range")
	}
	return t.ID, nil
}

// Store persists preferences.
type Store interface {
	// Get returns the user's preferences, or Defaults() (UpdatedAt 0) when nothing was saved.
	Get(ctx context.Context, user string) (Prefs, error)
	// Put validates and saves p, stamping UpdatedAt.
	Put(ctx context.Context, user string, p Prefs) (Prefs, error)
}

// DB is the SQLite-backed Store (table user_prefs; the user must exist in users_meta).
type DB struct {
	SQL interface {
		QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
		ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
	}
	Now func() time.Time
}

func (d DB) Get(ctx context.Context, user string) (Prefs, error) {
	var data string
	err := d.SQL.QueryRowContext(ctx, `SELECT data FROM user_prefs WHERE username=?`, user).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return Defaults(), nil
	}
	if err != nil {
		return Prefs{}, err
	}
	p := Defaults()
	if err := json.Unmarshal([]byte(data), &p); err != nil || Validate(&p) != nil {
		return Defaults(), nil // corrupt or outdated row: behave as "nothing saved"
	}
	return p, nil
}

func (d DB) Put(ctx context.Context, user string, p Prefs) (Prefs, error) {
	if err := Validate(&p); err != nil {
		return Prefs{}, err
	}
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	p.UpdatedAt = now().UnixMilli()
	b, err := json.Marshal(p)
	if err != nil {
		return Prefs{}, err
	}
	if len(b) > MaxBytes {
		return Prefs{}, bad("too large")
	}
	_, err = d.SQL.ExecContext(ctx, `INSERT INTO user_prefs(username, data, updated_at) VALUES(?,?,?)
		ON CONFLICT(username) DO UPDATE SET data=excluded.data, updated_at=excluded.updated_at`, user, string(b), p.UpdatedAt)
	return p, err
}

// Mem is an in-memory Store (demo mode, where there is no database).
type Mem struct {
	mu sync.Mutex
	m  map[string]Prefs
}

func (m *Mem) Get(_ context.Context, user string) (Prefs, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.m[user]; ok {
		return p, nil
	}
	return Defaults(), nil
}

func (m *Mem) Put(_ context.Context, user string, p Prefs) (Prefs, error) {
	if err := Validate(&p); err != nil {
		return Prefs{}, err
	}
	p.UpdatedAt = time.Now().UnixMilli()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.m == nil {
		m.m = map[string]Prefs{}
	}
	m.m[user] = p
	return p, nil
}
