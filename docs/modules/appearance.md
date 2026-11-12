# Module: Appearance (themes and preferences)

Route `/settings/appearance` (quick toggle in the top bar) · Roles: user (own), admin (global default) · Backend: `internal/appearance` · API: [`/appearance/*`](../API.md#appearance) · Schema: [`schemas/theme.schema.json`](../../schemas/theme.schema.json)

Requirement: *change the page style — colours, fonts, etc.* Yubeos' default look is TrueNAS-inspired; the theme engine makes every visual decision a token.

## 1. What can be customised

| Group | Controls |
|---|---|
| **Colour** | Background / surfaces (3 elevations), border, text, muted text, **accent**, accent-contrast, success / warning / danger / info, 8 chart colours; light / dark base |
| **Typography** | UI font, monospace font (terminal, code), display font (headings), base size (12–18 px), line-height, weight of headings |
| **Shape & density** | Corner radius (0–16 px), density `compact / comfortable / spacious`, card elevation (flat / soft / outlined) |
| **Layout** | Sidebar position (left/right), collapsed-by-default, sidebar width (200–320), top-bar visible, content max-width |
| **Motion** | Animations on/off (default follows `prefers-reduced-motion`) |
| **Background** | none · solid · gradient · image (uploaded, ≤ 3 MB, downscaled) · *generative* (see §5, post-1.0) |
| **Data display** | Temperature °C/°F, throughput MB/s vs Mbit/s, 12/24 h, date format, language (`en`, `it`) |

## 2. Token model

Themes are **pure data** compiled to CSS custom properties on `:root`:

```css
:root[data-theme="yubeos-blue"] {
  --y-bg:#0f1318; --y-surface-1:#161b22; --y-surface-2:#1d242d; --y-border:#2a3340;
  --y-text:#e6edf3; --y-text-muted:#8b98a9;
  --y-accent:#0095d5; --y-accent-contrast:#ffffff;
  --y-success:#3fb950; --y-warning:#d29922; --y-danger:#f85149; --y-info:#58a6ff;
  --y-chart-1:#0095d5; --y-chart-2:#3fb950; /* … 8 */
  --y-font-ui:"Inter", system-ui, sans-serif;
  --y-font-mono:"JetBrains Mono", ui-monospace, monospace;
  --y-font-display:"Space Grotesk", var(--y-font-ui);
  --y-font-size:14px; --y-radius:8px; --y-density:1; --y-sidebar-width:248px;
}
```

Rules: components reference **only** `var(--y-*)`; no hard-coded colours in component CSS (lint rule `no-raw-colors`). Charts and xterm read tokens at runtime so they re-theme live (xterm theme object derived from tokens: background, foreground, cursor, 16 ANSI colours — each theme may override the ANSI palette).

## 3. Built-in presets

| Preset | Description |
|---|---|
| **Yubeos Blue** *(default)* | Dark neutral, TrueNAS-style blue accent, card dashboard |
| **Yubeos Light** | Light counterpart |
| **Yubeos Midnight** | Palette from the original command-centre manual: near-black navy `#06080f`, cyan `#00d4ff`, green `#00f5a0`, amber `#ffb703`, purple `#a855f7` (Inter / JetBrains Mono / Space Grotesk) |
| **High Contrast** | WCAG AAA contrast, thicker borders, no transparency |
| **Solarized / Nord / Dracula** | Community-familiar palettes (also applied to terminal ANSI) |

Presets ship in the binary and are read-only; users **duplicate → edit → save as custom**.

## 4. Behaviour

- **Live preview:** editing a token updates the whole UI instantly (writes CSS variables in a scoped preview layer); *Save* persists, *Revert* discards.
- **Accessibility guard:** the editor computes contrast ratios (text/background, accent/background, accent-contrast/accent) and warns below WCAG AA (4.5:1 text, 3:1 UI); saving a failing theme is allowed but flagged.
- **Scope & precedence:** `user preference` > `admin global default` > `built-in default`. A user can follow *system* (light/dark via `prefers-color-scheme`) by choosing a light+dark pair.
- **Flash prevention:** the last applied theme tokens are cached in `localStorage` and applied by a 1 KB inline-free bootstrap script *before* first paint (served as a hashed static file, CSP-safe).
- **Import / export:** themes as `*.yubeos-theme.json`, validated against the schema (unknown keys rejected, colour formats `#rgb/#rrggbb/#rrggbbaa` only, numeric ranges clamped). Shareable on GitHub.
- **Fonts:** Inter, JetBrains Mono, Space Grotesk, IBM Plex Sans/Mono and a system stack are **bundled and self-hosted** (offline-first, no CDN). Users may upload `.woff2` fonts (≤ 2 MB, validated magic bytes, stored in `/var/lib/yubeos/fonts`, served `font-src 'self'`, licence acknowledgement checkbox). *No external font CDNs, ever* (privacy, offline server).
- **No custom CSS/JS in 1.0** (security; see [SECURITY §6](../SECURITY.md#6-module-specific-controls)). An "advanced: extra CSS" field is tracked as TD-004 and would be admin-only with sanitisation.

## 5. Generative background (optional, post-1.0)

Seeded, deterministic canvas animation (flow-field/particles) as a background option: own tiny PRNG (seed shown and editable → reproducible), 30 fps cap, paused when tab hidden or `prefers-reduced-motion`, off by default, ≤ 2 % CPU budget, drawn behind content with `pointer-events: none`. Implemented without a heavy library (plain 2D canvas). The same seeded-art approach can generate the project logo variants and README banners.

## 6. Acceptance criteria (Phase 1 shell + Phase 1b editor)

1. Switching preset re-themes shell, charts, tables, dialogs and terminal without reload and without layout shift.
2. Invalid theme JSON (bad colour, huge numbers, extra keys, script in strings) is rejected with field-level errors.
3. Reload shows the saved theme on first paint (no flash) on Chrome, Firefox, Safari.
4. All presets pass contrast checks except where documented (Solarized accent).
5. Axe-core accessibility scan: 0 critical issues on shell + appearance page for each preset.
