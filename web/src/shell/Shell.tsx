import { useEffect, useState } from "react";
import { NavLink, Outlet, useLocation } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMe } from "../auth/useMe";
import { ApiError, getSystemInfo, logout, setCsrfToken } from "../lib/api";
import { LANGS, useLang, useT, type Key } from "../i18n";
import { PRESETS } from "../theme/engine";
import { useTheme } from "../theme/useTheme";
import { Icon } from "./Icon";
import { navGroups } from "./nav";
import styles from "./Shell.module.css";

/** Re-exported for tests and public/theme-init.js parity: see src/theme/engine.ts. */
export { resolveTheme } from "../theme/engine";

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
  const t = useT();
  const { choice, custom, setChoice } = useTheme();
  const [lang, setLang] = useLang();
  const [menuOpen, setMenuOpen] = useState(false);
  const { pathname } = useLocation();

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
      <a className={styles.skip} href="#main">{t("shell.skip")}</a>

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
                {t(`nav.${item.id}` as Key)}
              </NavLink>
            ))}
          </div>
        ))}
      </nav>
      {menuOpen && <div className={styles.backdrop} onClick={() => setMenuOpen(false)} aria-hidden="true" />}

      <header className={styles.topbar}>
        <button type="button" className={styles.menuButton} aria-label={menuOpen ? t("shell.menuClose") : t("shell.menuOpen")}
                aria-expanded={menuOpen} aria-controls="main-nav" onClick={() => setMenuOpen((o) => !o)}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
            <path d="M4 6h16M4 12h16M4 18h16" />
          </svg>
        </button>

        {info.data ? (
          <>
            <span className={styles.meta}>{t("shell.host")} <b>{info.data.hostname}</b></span>
            <span className={styles.meta}>{t("shell.uptime")} <b>{formatUptime(info.data.uptimeSeconds ?? 0)}</b></span>
            <span className={styles.meta}>v{info.data.version}</span>
            {info.data.demoMode && <span className={styles.demo}>DEMO</span>}
          </>
        ) : err ? (
          <span className={`${styles.meta} ${styles.error}`} role="status">
            {err.status === 401 ? t("shell.notSignedIn")
              : err.status === 0 ? t("shell.unreachable")
              : t("shell.serverError", { status: err.status })}
            {err.status !== 401 && (
              <button type="button" className={styles.retry} onClick={() => void info.refetch()}>{t("shell.retry")}</button>
            )}
          </span>
        ) : null}

        <span className={styles.spacer} />
        {me.data && (
          <span className={styles.meta}>
            <b>{me.data.username}</b> ({me.data.role})
            <button type="button" className={styles.retry} onClick={() => void signOut()}>{t("shell.signOut")}</button>
          </span>
        )}
        <select className={styles.select} value={lang} onChange={(e) => setLang(e.target.value as "en" | "it")} aria-label={t("shell.language")}>
          {LANGS.map((l) => <option key={l.id} value={l.id}>{l.label}</option>)}
        </select>
        <select className={styles.select} value={choice} onChange={(e) => setChoice(e.target.value)} aria-label={t("shell.theme")}>
          <option value="system">{t("shell.themeSystem")}</option>
          {PRESETS.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          {custom.map((c) => <option key={c.id} value={`custom:${c.id}`}>{c.name}</option>)}
        </select>
      </header>

      <main id="main" tabIndex={-1} className={styles.main}>
        <Outlet />
      </main>
    </div>
  );
}
