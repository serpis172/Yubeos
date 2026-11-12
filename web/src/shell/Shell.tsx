import { useEffect, useState } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getSystemInfo } from "../lib/api";
import { Icon } from "./Icon";
import { navGroups } from "./nav";
import styles from "./Shell.module.css";

const themes = [
  { id: "yubeos-blue", label: "Yubeos Blue" },
  { id: "yubeos-light", label: "Yubeos Light" },
  { id: "yubeos-midnight", label: "Yubeos Midnight" },
] as const;

const THEME_KEY = "yubeos.theme"; // pre-auth cache only; real persistence arrives with /appearance (Phase 1)

function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  return d > 0 ? `${d}d ${h}h` : `${h}h ${Math.floor((seconds % 3600) / 60)}m`;
}

export function Shell() {
  const info = useQuery({ queryKey: ["system-info"], queryFn: getSystemInfo, refetchInterval: 30_000, retry: false });
  const [theme, setTheme] = useState<string>(() => {
    try { return localStorage.getItem(THEME_KEY) ?? "yubeos-blue"; } catch { return "yubeos-blue"; }
  });

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    try { localStorage.setItem(THEME_KEY, theme); } catch { /* storage may be unavailable */ }
  }, [theme]);

  return (
    <div className={styles.shell}>
      <nav className={styles.sidebar} aria-label="Main">
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

      <header className={styles.topbar}>
        {info.data ? (
          <>
            <span className={styles.meta}>host <b>{info.data.hostname}</b></span>
            <span className={styles.meta}>uptime <b>{formatUptime(info.data.uptimeSeconds ?? 0)}</b></span>
            <span className={styles.meta}>v{info.data.version}</span>
            {info.data.demoMode && <span className={styles.demo}>DEMO</span>}
          </>
        ) : info.isError ? (
          <span className={`${styles.meta} ${styles.error}`}>not signed in</span>
        ) : null}
        <span className={styles.spacer} />
        <label className={styles.meta}>
          <span className="sr-only" style={{ position: "absolute", left: -9999 }}>Theme</span>
          <select className={styles.select} value={theme} onChange={(e) => setTheme(e.target.value)} aria-label="Theme">
            {themes.map((t) => <option key={t.id} value={t.id}>{t.label}</option>)}
          </select>
        </label>
      </header>

      <main className={styles.main}>
        <Outlet />
      </main>
    </div>
  );
}
