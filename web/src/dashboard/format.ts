import type { TempUnit, Units } from "../prefs/PrefsProvider";

const num = (n: number, digits: number, lang?: string) => new Intl.NumberFormat(lang, { maximumFractionDigits: digits, minimumFractionDigits: digits }).format(n);

/** Binary-prefix size (KiB…): memory and disk capacities. */
export function formatBytes(n: number, lang?: string): string {
  if (!Number.isFinite(n)) return "–";
  const u = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
  let i = 0;
  while (Math.abs(n) >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return `${num(n, i === 0 ? 0 : n >= 100 ? 0 : n >= 10 ? 1 : 2, lang)} ${u[i]}`;
}

/** Decimal capacity (TB, as drives are sold). */
export function formatCapacity(n: number, lang?: string): string {
  const u = ["B", "kB", "MB", "GB", "TB", "PB"];
  let i = 0;
  while (n >= 1000 && i < u.length - 1) { n /= 1000; i++; }
  return `${num(n, n >= 100 ? 0 : 1, lang)} ${u[i]}`;
}

/** Throughput in bytes/s (MB/s) or bits/s (Mbit/s), decimal prefixes as network tools show them. */
export function formatRate(bytesPerSec: number, units: Units, lang?: string): string {
  if (!Number.isFinite(bytesPerSec)) return "–";
  const bits = units === "bits";
  let v = bits ? bytesPerSec * 8 : bytesPerSec;
  const u = bits ? ["bit/s", "kbit/s", "Mbit/s", "Gbit/s", "Tbit/s"] : ["B/s", "kB/s", "MB/s", "GB/s", "TB/s"];
  let i = 0;
  while (v >= 1000 && i < u.length - 1) { v /= 1000; i++; }
  return `${num(v, i === 0 ? 0 : v >= 100 ? 0 : v >= 10 ? 1 : 2, lang)} ${u[i]}`;
}

export function formatLinkSpeed(bitsPerSec: number, lang?: string): string {
  const g = bitsPerSec / 1e9;
  return g >= 1 ? `${num(g, g % 1 ? 1 : 0, lang)} Gbit/s` : `${num(bitsPerSec / 1e6, 0, lang)} Mbit/s`;
}

export function formatPercent(v: number, digits = 0, lang?: string): string {
  return Number.isFinite(v) ? `${num(v, digits, lang)} %` : "–";
}

export function formatTemp(c: number, unit: TempUnit, lang?: string): string {
  if (!Number.isFinite(c)) return "–";
  return unit === "f" ? `${num(c * 9 / 5 + 32, 0, lang)} °F` : `${num(c, 0, lang)} °C`;
}

export function formatHz(hz: number, lang?: string): string {
  return Number.isFinite(hz) ? `${num(hz / 1e9, 2, lang)} GHz` : "–";
}

export function formatWatts(w: number, lang?: string): string {
  return Number.isFinite(w) ? `${num(w, w >= 100 ? 0 : 1, lang)} W` : "–";
}

export function formatIops(v: number, lang?: string): string {
  return Number.isFinite(v) ? `${num(v, v >= 100 ? 0 : 1, lang)} IOPS` : "–";
}

export function formatUptime(seconds: number): string {
  if (!Number.isFinite(seconds)) return "–";
  const d = Math.floor(seconds / 86400), h = Math.floor((seconds % 86400) / 3600), m = Math.floor((seconds % 3600) / 60);
  return d > 0 ? `${d}d ${h}h` : h > 0 ? `${h}h ${m}m` : `${m}m`;
}
