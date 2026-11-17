import { useEffect, useState } from "react";
import { NavLink, Outlet, useLocation } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMe } from "../auth/useMe";
import { ApiError, getSystemInfo, logout, setCsrfToken } from "../lib/api";
import { Icon } from "./Icon";
import { navGroups } from "./nav";
import styles from "./Shell.module.css";

const themes = [
  { id: "system", label: "System" },
  { id: "yubeos-blue", label: "Yubeos Blue" },
  { id: "yubeos-light", label: "Yubeos Light" },
  { id: "yubeos-midnight", label: "Yubeos Midnight" },
] as const;

const THEME_KEY = "yubeos.theme"; // pre-auth cache only; real persistence arrives with /appearance (Phase 1e)

/** Same resolution rule as public/theme-init.js (which runs before first paint). */
export function resolveTheme(choice: string, prefersLight: boolean): string {
  return choice === "system" ? (prefersLight ? "yubeos-light" : "yubeos-blue") : choice;
}

function readStoredTheme(): string {
  try { return localStorage.getItem(THEME_KEY) ?? "system"; } catch { return "system"; }
}

function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  return d > 0 ? `${d}d ${h}h` : `${h}h ${Math.floor((seconds % 3600) / 60)}m`;
}

export function Shell() {
  const info = useQuery({
    queryKey: ["system-info"],
    queryFn: ({ signal }) => getSystemInfo(signal),
    refetchInterval: 30_000, // TanStack Query pauses polling in background tabs by default
    retry: false,
  });
  const me = useMe();
  const qc = useQueryClient();
  const [theme, setTheme] = useState<string>(readStoredTheme);
  const [menuOpen, setMenuOpen] = useState(false);
  const { pathname } = useLocation();

  useEffect(() => {
    const mq = window.matchMedia?.("(prefers-color-scheme: light)");
    const apply = () => { document.documentElement.dataset.theme = resolveTheme(theme, !!mq?.matches); };
    apply();
    if (theme === "system") {
      mq?.addEventListener?.("change", apply);
      return () => mq?.removeEventListener?.("change", apply);
    }
  }, [theme]);

  const chooseTheme = (value: string) => {
    setTheme(value);
    try { localStorage.setItem(THEME_KEY, value); } catch { /* storage may be unavailable */ }
  };

  // Close the drawer on navigation and on Escape.
  useEffect(() => setMenuOpen(false), [pathname]);
  useEffect(() => {
    if (!menuOpen) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") setMenuOpen(false); };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [menuOpen]);

  const err = info.error instanceof ApiError ? info.error : null;

  async function signOut() {
    try { await logout(); } finally {
      setCsrfToken("");
      qc.clear();
      window.location.assign("/");
    }
  }

  return (
    <div className={styles.shell}>
      <a className={styles.skip} href="#main">Skip to content</a>

      <nav id="main-nav" aria-label="Main" className={`${styles.sidebar} ${menuOpen ? styles.sidebarOpen : ""}`}>
        <div className={styles.brand}>
          <span className={styles.logo} aria-hidden="true">Y</span>
          Yubeos
        </div>
        {navGroups.map((group) => (
          <div key={group.id} className={styles.group}>
            {group.items.map((item) => (
              <NavLink key={item.id} to={item.to} end={item.to === "/"}
                       className={({ isActive }) => `${styles.link} ${isActive ? styles.active : ""}`}>
                <Icon name={item.icon} />
                {item.label}
              </NavLink>
            ))}
          </div>
        ))}
      </nav>
      {menuOpen && <div className={styles.backdrop} onClick={() => setMenuOpen(false)} aria-hidden="true" />}

      <header className={styles.topbar}>
        <button type="button" className={styles.menuButton} aria-label={menuOpen ? "Close menu" : "Open menu"}
                aria-expanded={menuOpen} aria-controls="main-nav" onClick={() => setMenuOpen((o) => !o)}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
            <path d="M4 6h16M4 12h16M4 18h16" />
          </svg>
        </button>

        {info.data ? (
          <>
            <span className={styles.meta}>host <b>{info.data.hostname}</b></span>
            <span className={styles.meta}>uptime <b>{formatUptime(info.data.uptimeSeconds ?? 0)}</b></span>
            <span className={styles.meta}>v{info.data.version}</span>
            {info.data.demoMode && <span className={styles.demo}>DEMO</span>}
          </>
        ) : err ? (
          <span className={`${styles.meta} ${styles.error}`} role="status">
            {err.status === 401 ? "not signed in"
              : err.status === 0 ? "server unreachable"
              : `server error (${err.status})`}
            {err.status !== 401 && (
              <button type="button" className={styles.retry} onClick={() => void info.refetch()}>Retry</button>
            )}
          </span>
        ) : null}

        <span className={styles.spacer} />
        {me.data && (
          <span className={styles.meta}>
            <b>{me.data.username}</b> ({me.data.role})
            <button type="button" className={styles.retry} onClick={() => void signOut()}>Sign out</button>
          </span>
        )}
        <select className={styles.select} value={theme} onChange={(e) => chooseTheme(e.target.value)} aria-label="Theme">
          {themes.map((t) => <option key={t.id} value={t.id}>{t.label}</option>)}
        </select>
      </header>

      <main id="main" tabIndex={-1} className={styles.main}>
        {me.data?.enrollmentRequired && (
          <p className={styles.banner} role="status">
            Your account must use a second factor. Enrolment arrives in the next release; until then most sections stay locked.
          </p>
        )}
        <Outlet />
      </main>
    </div>
  );
}
