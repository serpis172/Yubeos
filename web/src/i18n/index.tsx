import { useSyncExternalStore } from "react";
import en, { type Dict, type Key } from "./en";
import it from "./it";

export type Lang = "en" | "it";
export const LANGS: { id: Lang; label: string }[] = [
  { id: "en", label: en["lang.name"] },
  { id: "it", label: it["lang.name"] },
];
const dicts: Record<Lang, Dict> = { en, it };
const KEY = "yubeos.lang";

function detect(): Lang {
  try {
    const saved = localStorage.getItem(KEY);
    if (saved === "en" || saved === "it") return saved;
  } catch { /* storage unavailable */ }
  return typeof navigator !== "undefined" && navigator.language?.toLowerCase().startsWith("it") ? "it" : "en";
}

let current: Lang = detect();
const listeners = new Set<() => void>();

export function getLang(): Lang { return current; }

export const subscribeLang = (cb: () => void) => { listeners.add(cb); return () => listeners.delete(cb); };

export const hasKey = (k: string): k is Key => k in en;

export function setLang(l: Lang): void {
  current = l;
  try { localStorage.setItem(KEY, l); } catch { /* ignore */ }
  document.documentElement.lang = l;
  listeners.forEach((fn) => fn());
}

/** Translate outside React (messages built in handlers). Prefer useT() in components so they re-render on change. */
export function t(key: Key, vars?: Record<string, string | number>): string {
  let s: string = dicts[current][key] ?? en[key];
  if (vars) for (const [k, v] of Object.entries(vars)) s = s.split(`{${k}}`).join(String(v));
  return s;
}

/** Subscribes the component to language changes and returns t. */
export function useT(): typeof t {
  useSyncExternalStore((cb) => { listeners.add(cb); return () => listeners.delete(cb); }, getLang);
  return t;
}

export function useLang(): [Lang, (l: Lang) => void] {
  const l = useSyncExternalStore((cb) => { listeners.add(cb); return () => listeners.delete(cb); }, getLang);
  return [l, setLang];
}

export type { Key };
