/** Built-in themes as data (mirrors tokens.css; this file is the only TypeScript allowed to hold raw colours). */
export interface Theme {
  schemaVersion: 1;
  id: string;
  name: string;
  author?: string;
  base: "dark" | "light";
  colors: {
    bg: string; surface1: string; surface2: string; border: string; text: string; textMuted: string;
    accent: string; accentContrast: string; success: string; warning: string; danger: string; info: string;
    chart: string[];
  };
  typography: { ui: string; mono: string; display?: string; baseSizePx?: number; lineHeight?: number; headingWeight?: number };
  shape?: { radiusPx?: number; density?: "compact" | "comfortable" | "spacious"; elevation?: "flat" | "soft" | "outlined" };
}

const CHART = ["#0095d5", "#3fb950", "#d29922", "#f85149", "#a371f7", "#58a6ff", "#db61a2", "#79c0ff"];

export const FONT_UI = [
  { id: "inter", label: "Inter", value: "Inter Variable, Inter, system-ui, sans-serif" },
  { id: "grotesk", label: "Space Grotesk", value: "Space Grotesk Variable, Space Grotesk, system-ui, sans-serif" },
  { id: "system", label: "system-ui", value: "system-ui, -apple-system, Segoe UI, Roboto, sans-serif" },
] as const;
export const FONT_MONO = [
  { id: "jetbrains", label: "JetBrains Mono", value: "JetBrains Mono Variable, JetBrains Mono, ui-monospace, Menlo, monospace" },
  { id: "system", label: "ui-monospace", value: "ui-monospace, Menlo, Consolas, monospace" },
] as const;

const typography = { ui: FONT_UI[0].value, mono: FONT_MONO[0].value, display: FONT_UI[1].value, baseSizePx: 14, lineHeight: 1.5, headingWeight: 600 };

export const PRESETS: Theme[] = [
  {
    schemaVersion: 1, id: "yubeos-blue", name: "Yubeos Blue", base: "dark",
    colors: { bg: "#0f1318", surface1: "#161b22", surface2: "#1d242d", border: "#2a3340", text: "#e6edf3", textMuted: "#8b98a9", accent: "#0095d5",
      accentContrast: "#ffffff", success: "#3fb950", warning: "#d29922", danger: "#f85149", info: "#58a6ff", chart: CHART },
    typography, shape: { radiusPx: 8, density: "comfortable", elevation: "soft" },
  },
  {
    schemaVersion: 1, id: "yubeos-light", name: "Yubeos Light", base: "light",
    colors: { bg: "#f4f6f9", surface1: "#ffffff", surface2: "#eef1f5", border: "#d5dbe3", text: "#1b2430", textMuted: "#5b6878", accent: "#0078b0",
      accentContrast: "#ffffff", success: "#1a7f37", warning: "#9a6700", danger: "#cf222e", info: "#0969da",
      chart: ["#0078b0", "#1a7f37", "#9a6700", "#cf222e", "#8250df", "#0969da", "#bf3989", "#0550ae"] },
    typography, shape: { radiusPx: 8, density: "comfortable", elevation: "soft" },
  },
  {
    schemaVersion: 1, id: "yubeos-midnight", name: "Yubeos Midnight", base: "dark",
    colors: { bg: "#06080f", surface1: "#0d1220", surface2: "#141b2e", border: "#1f2942", text: "#e8ecf7", textMuted: "#8590ad", accent: "#00d4ff",
      accentContrast: "#04141a", success: "#00f5a0", warning: "#ffb703", danger: "#ff4d6d", info: "#a855f7",
      chart: ["#00d4ff", "#00f5a0", "#ffb703", "#ff4d6d", "#a855f7", "#4cc9f0", "#f72585", "#7209b7"] },
    typography, shape: { radiusPx: 8, density: "comfortable", elevation: "soft" },
  },
];

export const presetById = (id: string): Theme | undefined => PRESETS.find((p) => p.id === id);
