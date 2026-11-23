import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { contrastRatio, exportTheme, importTheme, PRESETS, slug, themeVars, toHex6, validateTheme } from "./engine";

describe("theme engine", () => {
  it("accepts every built-in preset", () => {
    for (const p of PRESETS) expect(validateTheme(p).ok).toBe(true);
  });

  it("round-trips through export/import", () => {
    const r = importTheme(exportTheme(PRESETS[0]!));
    expect(r.ok).toBe(true);
  });

  it("rejects unknown keys, bad colours and bad ranges with readable errors", () => {
    const bad = { ...PRESETS[0]!, extra: 1, colors: { ...PRESETS[0]!.colors, bg: "red" }, typography: { ...PRESETS[0]!.typography, baseSizePx: 99 } };
    const r = validateTheme(bad);
    expect(r.ok).toBe(false);
    if (!r.ok) {
      expect(r.errors.join("\n")).toMatch(/extra: unknown/);
      expect(r.errors.join("\n")).toMatch(/colors\.bg/);
      expect(r.errors.join("\n")).toMatch(/baseSizePx/);
    }
  });

  it("rejects non-JSON and non-objects", () => {
    expect(importTheme("{nope").ok).toBe(false);
    expect(validateTheme([]).ok).toBe(false);
  });

  it("computes WCAG contrast", () => {
    expect(contrastRatio("#000000", "#ffffff")).toBeCloseTo(21, 0);
    expect(contrastRatio("#777777", "#777777")).toBeCloseTo(1, 5);
  });

  it("maps a theme to --y-* variables only", () => {
    const v = themeVars(PRESETS[2]!);
    expect(v["--y-accent"]).toBe("#00d4ff");
    expect(Object.keys(v).every((k) => k.startsWith("--y-"))).toBe(true);
  });

  it("normalises colours and slugs", () => {
    expect(toHex6("#abc")).toBe("#aabbcc");
    expect(toHex6("#AABBCCDD")).toBe("#aabbcc");
    expect(slug("My  Theme!")).toBe("my-theme");
  });
});

describe("preset CSS parity", () => {
  const tokensCss = readFileSync(resolve(process.cwd(), "src/theme/tokens.css"), "utf8");
  it("tokens.css defines the chart colours of every preset (charts are invisible without them)", () => {
    for (const p of PRESETS) {
      const block = tokensCss.slice(tokensCss.indexOf(`:root[data-theme="${p.id}"]`));
      p.colors.chart.forEach((c, i) => expect(block, `${p.id} chart ${i + 1}`).toContain(`--y-chart-${i + 1}: ${c}`));
    }
  });
});
