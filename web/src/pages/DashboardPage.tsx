import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { CpuCard, DisksCard, GpuCard, NetCard, RamCard, SystemCard } from "../dashboard/cards";
import { useLiveMetrics, type LiveOptions } from "../dashboard/live";
import { useStoreVersion } from "../dashboard/store";
import styles from "../dashboard/Dashboard.module.css";
import { useT } from "../i18n";
import { getMetricCatalog } from "../lib/api";
import { usePrefs } from "../prefs/PrefsProvider";

const CONN = { live: "dash.live", connecting: "dash.connecting", reconnecting: "dash.reconnecting", polling: "dash.polling" } as const;

export function DashboardPage({ live }: { live?: LiveOptions }) {
  const t = useT();
  const { units, temperature } = usePrefs();
  const store = useLiveMetrics(live);
  useStoreVersion(store); // re-render on every frame (≈1 Hz)
  const [showVirtual, setShowVirtual] = useState(false);
  const catalog = useQuery({ queryKey: ["metric-catalog"], queryFn: ({ signal }) => getMetricCatalog(signal), refetchInterval: 30_000 });
  useEffect(() => { document.title = `${t("dash.title")} · Yubeos`; }, [t]);

  const devices = useMemo(() => catalog.data?.devices ?? [], [catalog.data]);
  const stale = store.connection === "reconnecting";
  const common = { store, units, temp: temperature, stale };
  const cpus = devices.filter((d) => d.kind === "cpu");
  const nets = devices.filter((d) => d.kind === "net" && (showVirtual || !d.hidden));
  const gpus = devices.filter((d) => d.kind === "gpu");
  const disks = devices.filter((d) => d.kind === "disk");
  const problems = (catalog.data?.providers ?? []).filter((p) => !p.ok);
  const hasVirtual = devices.some((d) => d.kind === "net" && d.hidden);

  return (
    <div className={styles.page}>
      <div className={styles.top}>
        <h1>{t("dash.title")}</h1>
        <div className={styles.tools}>
          {hasVirtual && (
            <label className={styles.check}>
              <input type="checkbox" checked={showVirtual} onChange={(e) => setShowVirtual(e.target.checked)} />
              {t("dash.showVirtual")}
            </label>
          )}
          <span className={`${styles.pill} ${store.connection === "live" ? styles.live : styles.warn}`} role="status">{t(CONN[store.connection])}</span>
        </div>
      </div>

      {problems.map((p) => <p key={p.name} className={styles.notice} role="status">{t("dash.providerDown", { name: p.name, reason: p.reason ?? "" })}</p>)}

      {catalog.isPending && <div className={styles.skeleton} aria-busy="true" />}
      {catalog.isError && <p className={styles.notice} role="alert">{t("sec.loadFailed")}</p>}

      <div className={styles.grid}>
        {cpus.map((d) => <CpuCard key={d.id} {...common} device={d} />)}
        <RamCard {...common} />
        {gpus.map((d) => <GpuCard key={d.id} {...common} device={d} />)}
        {nets.map((d) => <NetCard key={d.id} {...common} device={d} />)}
      </div>
      {catalog.data && <div className={styles.grid}><DisksCard {...common} devices={disks} /></div>}
      <div className={styles.grid}><SystemCard {...common} /></div>
    </div>
  );
}
