# UI Design

Visual language: **TrueNAS-inspired** — persistent left sidebar, top bar with system context, card-based dashboard, dense data tables, a single strong accent colour. The look is fully token-driven ([Appearance](modules/appearance.md)). The original hand-drawn sketches are the functional source of truth for layout; this document turns them into a spec.

## 1. Application shell

```
┌──────────────┬──────────────────────────────────────────────────────────────────────┐
│ [logo] Yubeos│ hostname · uptime · ⚠ alerts · 🎨 theme · user ▾                       │ top bar 56px
├──────────────┼──────────────────────────────────────────────────────────────────────┤
│ Dashboard    │                                                                        │
│ File Explorer│                           page content                                 │
│ Disk Manager │            (max-width token, cards on 12-col grid)                     │
│ Terminal     │                                                                        │
│ ──────────── │                                                                        │
│ Add-ons      │                                                                        │
│ Repo-Papers  │                                                                        │
│ ──────────── │                                                                        │
│  ▸ Add-on 1  │  ← installed add-ons (dynamic, from /addons where sidebar=true)        │
│  ▸ Add-on 2  │                                                                        │
│ ──────────── │                                                                        │
│ Settings     │  Appearance · Security · System                                        │
└──────────────┴──────────────────────────────────────────────────────────────────────┘
```

- Sidebar: 248 px (token), collapsible to 64 px icons, groups separated as in the sketch (core sections · Add-ons & Repo-Papers · installed add-ons). Active item highlighted with accent bar; keyboard `g d` / `g f` / `g k` / `g t` shortcuts; `?` shows help.
- Mobile (< 900 px): sidebar becomes a drawer; cards stack; File Explorer becomes list view; Terminal usable with an on-screen extra-keys bar (Esc, Tab, Ctrl, arrows).
- Global: command palette (`Ctrl/Cmd-K`) to jump to sections/disks/shares/add-ons.

## 2. Page specifications (from the sketches)

### Dashboard
Row 1 — CPU cards: 1…N packages (name, `TOT n CORE`, util, temp, power, line chart); the third sketch card is the **per-core heat grid**.
Row 2 — RAM (gauge + chart) left, Network cards (per NIC; `67 MB/s`, `1 GbE`) right.
Row 3 — GPU strip(s): name, core, temp, VRAM, power.
Row 4 — Disk widgets (Disk 1, Disk 2 …) using aliases from Disk Manager.
Details: [dashboard](modules/dashboard.md).

### File Explorer
Breadcrumb → table (Name · Date · Type [+ Size]) with expandable folder rows → bottom action bar: **Upload**, **Download** (+ New folder, Rename, Delete in toolbar/context menu). Details: [files](modules/files.md).

### Disk Manager
Two-pane: disk list (left, "DISKS") and detail (right): editable name ✎ with hover card, attribute table (Type · Capacity · Occupied · Pool · Tag), **Health / Utilisation & Status** panel, then tabs **Pools · RAID · Sharing**. Details: [storage](modules/storage.md).

### Terminal
Tab strip + full-height xterm area; `+` new tab; status chip (connected / reconnecting / exited). Details: [terminal](modules/terminal.md).

### Add-ons
Segmented control **Installed | Catalogue | Settings(catalogues)**; catalogue grid with search/category; install dialog shows permissions & config form. Installed add-ons: status chip, start/stop/restart, update badge, logs drawer, open UI.

### Repo-Papers
Tabs **Repos | Papers | Topics | Sources**; window selector (7 d / 30 d / 90 d / 180 d) and topic chips as a sticky filter bar. Details: [repo-papers](modules/repo-papers.md).

### Appearance
Left: preset list + "My themes"; centre: live editor (colour pickers with contrast badges, font selectors, sliders); right: live preview pane (mini dashboard). Details: [appearance](modules/appearance.md).

## 3. Component inventory (`web/src/components`)

`Card`, `StatTile`, `Gauge`, `Sparkline/TimeChart` (uPlot wrapper, token-aware), `CoreHeatGrid`, `DataTable` (virtualised, sortable), `TreeRow`, `Breadcrumb`, `Tabs`, `Dialog`, `ConfirmDialog` (typed-name + step-up password), `Drawer`, `Toast`, `Tooltip`, `HoverCard`, `Badge/StatusChip`, `Form*` (schema-driven for add-on config), `EmptyState`, `Skeleton`, `ColorField`, `FontField`, `CommandPalette`, `JobProgress`.

Each component: props typed, tokens only, Storybook story (static build used for visual regression), keyboard + ARIA behaviour documented in the story.

## 4. Interaction principles

1. **Never act on stale data:** destructive flows re-fetch and show the *plan* first.
2. **Long tasks are visible:** every job appears in a global Jobs tray with progress and logs.
3. **Everything degrades:** each card has loading, empty, error and "capability missing" states.
4. **Copyable truth:** commands equivalent to GUI actions are shown (e.g. the `zpool create …` plan) — the GUI teaches, it doesn't hide.
5. **Consistent confirmations:** reversible = undo toast; irreversible = typed confirmation (+ password step-up for destructive class).

## 5. Internationalisation and formatting

i18next with namespaces per module; `en` (source) and `it` shipped in 1.0; ICU plurals; `Intl` for numbers, dates, units; no string concatenation for sentences; CI check fails on missing keys.

## 6. Performance budgets

Shell JS ≤ 400 KB gz; each module chunk ≤ 150 KB gz (xterm chunk ≤ 250 KB); charts redraw ≤ 4 ms/frame at 8 series; WS frames batched to one React commit per animation frame; tables virtualised; Lighthouse performance ≥ 90 (desktop, LAN).

## 7. Browser support

Last 2 versions of Chrome, Edge, Firefox, Safari (desktop + mobile). Features used: CSS custom properties, container queries, `:has()`, WebSocket, `fetch` streams, `File System Access` (optional enhancement only).
