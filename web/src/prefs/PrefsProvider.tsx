import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { getPreferences, putPreferences, type Preferences } from "../lib/api";
import { getLang, setLang, subscribeLang, type Lang } from "../i18n";
import { getThemeState, replaceThemeState, subscribeTheme, validateTheme, type Theme } from "../theme/engine";

export type Units = "bytes" | "bits";
export type TempUnit = "c" | "f";

interface Ctx {
  units: Units;
  temperature: TempUnit;
  setUnits: (u: Units) => void;
  setTemperature: (t: TempUnit) => void;
}

const UNITS_KEY = "yubeos.units";
const TEMP_KEY = "yubeos.temperature";

const read = <T extends string>(k: string, ok: T[], fallback: T): T => {
  try { const v = localStorage.getItem(k); if (v && (ok as string[]).includes(v)) return v as T; } catch { /* storage unavailable */ }
  return fallback;
};
const write = (k: string, v: string) => { try { localStorage.setItem(k, v); } catch { /* ignore */ } };

const PrefsContext = createContext<Ctx>({ units: "bytes", temperature: "c", setUnits: () => {}, setTemperature: () => {} });
export const usePrefs = () => useContext(PrefsContext);

/**
 * Keeps theme, language, units and temperature in sync with the server, per user.
 *  - First sign-in on a server with nothing saved: the browser's current settings are uploaded (migration).
 *  - Otherwise the server's settings win and are applied locally (and cached in localStorage for the pre-paint script).
 *  - Later local changes are uploaded after a short debounce. Last write wins.
 */
export function PrefsProvider({ children }: { children: ReactNode }) {
  const [units, setUnitsState] = useState<Units>(() => read(UNITS_KEY, ["bytes", "bits"], "bytes"));
  const [temperature, setTempState] = useState<TempUnit>(() => read(TEMP_KEY, ["c", "f"], "c"));
  const ready = useRef(false);   // initial server state applied
  const lastSent = useRef("");   // serialised last state known to the server
  const timer = useRef<number | undefined>(undefined);
  const latest = useRef({ units, temperature });
  latest.current = { units, temperature };

  const snapshot = useCallback((): Preferences => {
    const th = getThemeState();
    return {
      theme: th.choice, language: getLang() as Lang, units: latest.current.units, temperature: latest.current.temperature,
      customThemes: th.custom as unknown as Preferences["customThemes"], updatedAt: 0,
    };
  }, []);

  const push = useCallback(() => {
    if (!ready.current) return;
    const p = snapshot();
    const ser = JSON.stringify({ ...p, updatedAt: 0 });
    if (ser === lastSent.current) return;
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => {
      lastSent.current = ser;
      void putPreferences(p).catch(() => { lastSent.current = ""; /* retry on the next change */ });
    }, 600);
  }, [snapshot]);

  useEffect(() => {
    const ac = new AbortController();
    getPreferences(ac.signal).then((p) => {
      if (p.updatedAt === 0) {
        // nothing on the server yet: upload what this browser already has
        ready.current = true;
        lastSent.current = "";
        push();
        return;
      }
      const themes = p.customThemes.flatMap((t) => { const r = validateTheme(t); return r.ok ? [r.theme as Theme] : []; });
      replaceThemeState({ choice: p.theme, custom: themes });
      setLang(p.language as Lang);
      write(UNITS_KEY, p.units); write(TEMP_KEY, p.temperature);
      setUnitsState(p.units as Units); setTempState(p.temperature as TempUnit);
      latest.current = { units: p.units as Units, temperature: p.temperature as TempUnit };
      lastSent.current = JSON.stringify({ ...snapshot(), updatedAt: 0 });
      ready.current = true;
    }).catch(() => { ready.current = true; /* offline or old server: stay local */ });
    return () => { ac.abort(); window.clearTimeout(timer.current); };
  }, [push, snapshot]);

  useEffect(() => {
    const offs = [subscribeTheme(push), subscribeLang(push)];
    return () => offs.forEach((o) => o());
  }, [push]);

  const setUnits = useCallback((u: Units) => { write(UNITS_KEY, u); setUnitsState(u); latest.current.units = u; push(); }, [push]);
  const setTemperature = useCallback((t: TempUnit) => { write(TEMP_KEY, t); setTempState(t); latest.current.temperature = t; push(); }, [push]);
  const value = useMemo(() => ({ units, temperature, setUnits, setTemperature }), [units, temperature, setUnits, setTemperature]);
  return <PrefsContext.Provider value={value}>{children}</PrefsContext.Provider>;
}
