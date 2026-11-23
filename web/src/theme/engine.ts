import { PRESETS, presetById, type Theme } from "./presets";

export type { Theme };
export const THEME_KEY = "yubeos.theme"; // "system" | preset id | "custom:<id>"
export const CUSTOM_KEY = "yubeos.themes"; // JSON array of custom themes (validated on read)

// ---- validation (mirrors schemas/theme.schema.json; keep in sync) --------------------------------------------------
const HEX = /^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/;
const FONT = /^[A-Za-z0-9 ,'"_.-]{1,120}$/;
const ID = /^[a-z0-9][a-z0-9-]{1,47}$/;
const COLOR_KEYS = ["bg", "surface1", "surface2", "border", "text", "textMuted", "accent", "accentContrast", "success", "warning", "danger", "info"] as const;
export type ColorKey = (typeof COLOR_KEYS)[number];
export const COLOR_KEY_LIST: readonly ColorKey[] = COLOR_KEYS;

type Obj = Record<string, unknown>;
const isObj = (v: unknown): v is Obj => typeof v === "object" && v !== null && !Array.isArray(v);

function unknownKeys(o: Obj, allowed: string[], path: string, errors: string[]) {
  for (const k of Object.keys(o)) if (!allowed.includes(k)) errors.push(`${path}${k}: unknown property`);
}

function num(o: Obj, k: string, min: number, max: number, path: string, errors: string[], int = false) {
  const v = o[k];
  if (v === undefined) return;
  if (typeof v !== "number" || v < min || v > max || (int && !Number.isInteger(v))) errors.push(`${path}${k}: must be ${int ? "an integer" : "a number"} ${min}..${max}`);
}

export type Validation = { ok: true; theme: Theme } | { ok: false; errors: string[] };

export function validateTheme(input: unknown): Validation {
  const errors: string[] = [];
  if (!isObj(input)) return { ok: false, errors: ["theme must be a JSON object"] };
  unknownKeys(input, ["schemaVersion", "id", "name", "author", "base", "colors", "typography", "shape", "layout", "motion", "background"], "", errors);
  if (input.schemaVersion !== 1) errors.push("schemaVersion: must be 1");
  if (typeof input.id !== "string" || !ID.test(input.id)) errors.push("id: lowercase letters, digits and dashes, 2..48 characters");
  if (typeof input.name !== "string" || input.name.length < 1 || input.name.length > 60) errors.push("name: 1..60 characters");
  if (input.author !== undefined && (typeof input.author !== "string" || input.author.length > 80)) errors.push("author: at most 80 characters");
  if (input.base !== "dark" && input.base !== "light") errors.push('base: must be "dark" or "light"');

  const c = input.colors;
  if (!isObj(c)) errors.push("colors: required object");
  else {
    unknownKeys(c, [...COLOR_KEYS, "chart", "terminal"], "colors.", errors);
    for (const k of COLOR_KEYS) if (typeof c[k] !== "string" || !HEX.test(c[k])) errors.push(`colors.${k}: must be a hex colour like #1a2b3c`);
    if (!Array.isArray(c.chart) || c.chart.length !== 8 || !c.chart.every((x) => typeof x === "string" && HEX.test(x))) errors.push("colors.chart: exactly 8 hex colours");
  }
  const ty = input.typography;
  if (!isObj(ty)) errors.push("typography: required object");
  else {
    unknownKeys(ty, ["ui", "mono", "display", "baseSizePx", "lineHeight", "headingWeight"], "typography.", errors);
    for (const k of ["ui", "mono", "display"]) {
      if (ty[k] === undefined && k === "display") continue;
      if (typeof ty[k] !== "string" || !FONT.test(ty[k])) errors.push(`typography.${k}: font family list (letters, digits, spaces, commas)`);
    }
    num(ty, "baseSizePx", 12, 18, "typography.", errors);
    num(ty, "lineHeight", 1.2, 2, "typography.", errors);
    num(ty, "headingWeight", 400, 800, "typography.", errors, true);
  }
  if (input.shape !== undefined) {
    const s = input.shape;
    if (!isObj(s)) errors.push("shape: must be an object");
    else {
      unknownKeys(s, ["radiusPx", "density", "elevation"], "shape.", errors);
      num(s, "radiusPx", 0, 16, "shape.", errors);
      if (s.density !== undefined && !["compact", "comfortable", "spacious"].includes(String(s.density))) errors.push("shape.density: compact | comfortable | spacious");
      if (s.elevation !== undefined && !["flat", "soft", "outlined"].includes(String(s.elevation))) errors.push("shape.elevation: flat | soft | outlined");
    }
  }
  return errors.length ? { ok: false, errors } : { ok: true, theme: input as unknown as Theme };
}

// ---- colour helpers ------------------------------------------------------------------------------------------------
/** Normalises #rgb / #rrggbb / #rrggbbaa to #rrggbb (what <input type="color"> needs). */
export function toHex6(c: string): string {
  if (/^#[0-9a-fA-F]{3}$/.test(c)) return "#" + [...c.slice(1)].map((x) => x + x).join("").toLowerCase();
  return c.slice(0, 7).toLowerCase();
}

function luminance(hex: string): number {
  const h = toHex6(hex).slice(1);
  const [r, g, b] = [0, 2, 4].map((i) => {
    const v = parseInt(h.slice(i, i + 2), 16) / 255;
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  }) as [number, number, number];
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** WCAG contrast ratio between two colours (1..21). */
export function contrastRatio(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

// ---- CSS variables -------------------------------------------------------------------------------------------------
const VAR_NAMES: Record<ColorKey, string> = {
  bg: "--y-bg", surface1: "--y-surface-1", surface2: "--y-surface-2", border: "--y-border", text: "--y-text", textMuted: "--y-text-muted",
  accent: "--y-accent", accentContrast: "--y-accent-contrast", success: "--y-success", warning: "--y-warning", danger: "--y-danger", info: "--y-info",
};

/** The token overrides a theme produces. Only tokens that exist in tokens.css (plus chart colours). */
export function themeVars(t: Theme): Record<string, string> {
  const v: Record<string, string> = {};
  for (const k of COLOR_KEYS) v[VAR_NAMES[k]] = t.colors[k];
  t.colors.chart.forEach((c, i) => { v[`--y-chart-${i + 1}`] = c; });
  v["--y-font-ui"] = t.typography.ui;
  v["--y-font-mono"] = t.typography.mono;
  if (t.typography.display) v["--y-font-display"] = t.typography.display;
  if (t.typography.baseSizePx) v["--y-font-size"] = `${t.typography.baseSizePx}px`;
  if (t.typography.lineHeight) v["--y-line-height"] = String(t.typography.lineHeight);
  if (t.typography.headingWeight) v["--y-heading-weight"] = String(t.typography.headingWeight);
  if (t.shape?.radiusPx !== undefined) v["--y-radius"] = `${t.shape.radiusPx}px`;
  return v;
}

// ---- store (localStorage, shared between the shell and the Appearance page) ---------------------------------------------
interface State { choice: string; custom: Theme[] }

function readCustom(): Theme[] {
  try {
    const raw = JSON.parse(localStorage.getItem(CUSTOM_KEY) ?? "[]") as unknown;
    if (!Array.isArray(raw)) return [];
    return raw.map(validateTheme).flatMap((r) => (r.ok ? [r.theme] : []));
  } catch { return []; }
}

function readChoice(): string {
  try { return localStorage.getItem(THEME_KEY) ?? "system"; } catch { return "system"; }
}

let state: State = { choice: readChoice(), custom: readCustom() };
const listeners = new Set<() => void>();
let applied: string[] = [];

export const getThemeState = (): State => state;
export const subscribeTheme = (cb: () => void) => { listeners.add(cb); return () => listeners.delete(cb); };

function emit(next: State) {
  state = next;
  listeners.forEach((l) => l());
}

/** "system" follows the OS; otherwise the preset id or custom theme. Returns the preset that supplies untouched tokens. */
export function resolveTheme(choice: string, prefersLight: boolean): string {
  if (choice === "system") return prefersLight ? "yubeos-light" : "yubeos-blue";
  return choice;
}

/** Applies a theme to <html>: preset via data-theme, custom via data-theme of its base + inline variable overrides. */
export function applyThemeObject(t: Theme | undefined, presetId: string): void {
  const root = document.documentElement;
  for (const name of applied) root.style.removeProperty(name);
  applied = [];
  if (t) {
    root.dataset.theme = t.base === "light" ? "yubeos-light" : "yubeos-blue";
    for (const [k, val] of Object.entries(themeVars(t))) { root.style.setProperty(k, val); applied.push(k); }
    if (t.shape?.density) root.dataset.density = t.shape.density;
    if (t.shape?.elevation) root.dataset.elevation = t.shape.elevation;
  } else {
    root.dataset.theme = presetId;
    delete root.dataset.density;
    delete root.dataset.elevation;
  }
}

export function applyChoice(choice: string, prefersLight: boolean, custom: Theme[] = state.custom): void {
  if (choice.startsWith("custom:")) {
    const t = custom.find((c) => c.id === choice.slice(7));
    if (t) return applyThemeObject(t, "");
    choice = "system";
  }
  const id = resolveTheme(choice, prefersLight);
  applyThemeObject(undefined, presetById(id) ? id : "yubeos-blue");
}

export function setChoice(choice: string): void {
  try { localStorage.setItem(THEME_KEY, choice); } catch { /* storage unavailable */ }
  emit({ ...state, choice });
}

export function saveCustom(theme: Theme): void {
  const rest = state.custom.filter((c) => c.id !== theme.id);
  const custom = [...rest, theme].slice(-12);
  try { localStorage.setItem(CUSTOM_KEY, JSON.stringify(custom)); } catch { /* ignore */ }
  emit({ ...state, custom });
}

export function deleteCustom(id: string): void {
  const custom = state.custom.filter((c) => c.id !== id);
  try { localStorage.setItem(CUSTOM_KEY, JSON.stringify(custom)); } catch { /* ignore */ }
  emit({ choice: state.choice === `custom:${id}` ? "system" : state.choice, custom });
}

export const exportTheme = (t: Theme): string => JSON.stringify(t, null, 2);

export function importTheme(text: string): Validation {
  let raw: unknown;
  try { raw = JSON.parse(text); } catch { return { ok: false, errors: ["not valid JSON"] }; }
  return validateTheme(raw);
}

export function slug(name: string): string {
  const s = name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 40);
  return s.length >= 2 ? s : `theme-${Date.now().toString(36)}`;
}

export { PRESETS, presetById };
export { FONT_MONO, FONT_UI } from "./presets";

/** Select a custom theme (after saving/importing it). */
export const setChoiceFor = (t: Theme): void => setChoice(`custom:${t.id}`);

/** Replaces the whole theme state with what the server holds (applied at sign-in). */
export function replaceThemeState(next: { choice: string; custom: Theme[] }): void {
  try {
    localStorage.setItem(THEME_KEY, next.choice);
    localStorage.setItem(CUSTOM_KEY, JSON.stringify(next.custom));
  } catch { /* storage unavailable */ }
  emit({ choice: next.choice, custom: next.custom });
}
