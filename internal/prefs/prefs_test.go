package prefs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const goodTheme = `{"schemaVersion":1,"id":"my-theme","name":"Mine","base":"dark",
 "colors":{"bg":"#0f1318","surface1":"#161b22","surface2":"#1d242d","border":"#2a3340","text":"#e6edf3","textMuted":"#8b98a9","accent":"#0095d5",
  "accentContrast":"#ffffff","success":"#3fb950","warning":"#d29922","danger":"#f85149","info":"#58a6ff",
  "chart":["#0095d5","#3fb950","#d29922","#f85149","#a371f7","#58a6ff","#db61a2","#79c0ff"]},
 "typography":{"ui":"Inter, system-ui, sans-serif","mono":"ui-monospace, Menlo, monospace","baseSizePx":14,"lineHeight":1.5},
 "shape":{"radiusPx":8,"density":"comfortable"}}`

func TestValidate(t *testing.T) {
	p := Prefs{CustomThemes: []json.RawMessage{json.RawMessage(goodTheme)}}
	if err := Validate(&p); err != nil || p.Theme != "system" || p.Units != "bytes" {
		t.Fatalf("good prefs: %v %+v", err, p)
	}
	for name, mut := range map[string]func(*Prefs){
		"language":    func(p *Prefs) { p.Language = "xx" },
		"theme":       func(p *Prefs) { p.Theme = "../etc" },
		"temperature": func(p *Prefs) { p.Temperature = "k" },
		"colour": func(p *Prefs) {
			p.CustomThemes = []json.RawMessage{json.RawMessage(strings.Replace(goodTheme, `"#0f1318"`, `"red"`, 1))}
		},
		"font": func(p *Prefs) {
			p.CustomThemes = []json.RawMessage{json.RawMessage(strings.Replace(goodTheme, `Inter, system-ui, sans-serif`, `x;} body{display:none`, 1))}
		},
		"unknown key": func(p *Prefs) {
			p.CustomThemes = []json.RawMessage{json.RawMessage(strings.Replace(goodTheme, `"name"`, `"evil":1,"name"`, 1))}
		},
		"range": func(p *Prefs) {
			p.CustomThemes = []json.RawMessage{json.RawMessage(strings.Replace(goodTheme, `"baseSizePx":14`, `"baseSizePx":99`, 1))}
		},
		"duplicate": func(p *Prefs) {
			p.CustomThemes = []json.RawMessage{json.RawMessage(goodTheme), json.RawMessage(goodTheme)}
		},
	} {
		q := Defaults()
		mut(&q)
		if err := Validate(&q); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
}

func TestMemStoreRoundTrip(t *testing.T) {
	var m Mem
	ctx := context.Background()
	if p, _ := m.Get(ctx, "a"); p.UpdatedAt != 0 {
		t.Fatal("empty store must return defaults")
	}
	in := Defaults()
	in.Language = "it"
	out, err := m.Put(ctx, "a", in)
	if err != nil || out.UpdatedAt == 0 {
		t.Fatalf("put: %v %+v", err, out)
	}
	if p, _ := m.Get(ctx, "a"); p.Language != "it" {
		t.Fatal("not stored")
	}
	if p, _ := m.Get(ctx, "b"); p.Language != "en" {
		t.Fatal("must be per user")
	}
}
