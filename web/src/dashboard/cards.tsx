import { getLang, useT } from "../i18n";
import type { MetricDevice } from "../lib/api";
import type { TempUnit, Units } from "../prefs/PrefsProvider";
import { formatBytes, formatCapacity, formatHz, formatIops, formatLinkSpeed, formatPercent, formatRate, formatTemp, formatUptime, formatWatts } from "./format";
import type { MetricsStore } from "./store";
import { Bar, Card, Gauge, HeatGrid, LineChart, Stat, StackedBar } from "./widgets";
import styles from "./Dashboard.module.css";

export interface CardProps { store: MetricsStore; units: Units; temp: TempUnit; stale: boolean }
const lang = () => getLang();
const dash = "–";
const val = (s: MetricsStore, k: string) => s.get(k);
const fmt = (v: number | undefined, f: (n: number) => string) => (v === undefined ? dash : f(v));

export function CpuCard({ store, temp, stale, device }: CardProps & { device: MetricDevice }) {
  const t = useT();
  const id = device.id;
  const logical = ((device.props?.logical as number[] | undefined) ?? []);
  const util = val(store, `cpu.${id}.util`);
  const cells = logical.map((n) => ({ id: n, pct: val(store, `cpu.core.${n}.util`) ?? 0 }));
  return (
    <Card title={t("dash.cpuSocket", { n: id })} subtitle={`${device.label} · ${t("dash.cores", { cores: String(device.props?.cores ?? "?"), threads: String(device.props?.threads ?? logical.length) })}`} stale={stale}>
      <div className={styles.row}>
        <Gauge value={util ?? 0} label={t("dash.util")}>{fmt(util, (v) => `${Math.round(v)}%`)}</Gauge>
        <div className={styles.stats}>
          <Stat label={t("dash.temp")} value={fmt(val(store, `cpu.${id}.temp`), (v) => formatTemp(v, temp, lang()))} />
          <Stat label={t("dash.power")} value={fmt(val(store, `cpu.${id}.power`), (v) => formatWatts(v, lang()))} />
          <Stat label={t("dash.freq")} value={fmt(val(store, `cpu.${id}.freq`), (v) => formatHz(v, lang()))} />
        </div>
      </div>
      <LineChart lines={[{ values: store.series(`cpu.${id}.util`), color: "var(--y-chart-1)" }]} max={100} label={`${t("dash.util")} (${t("dash.last5m")})`} />
      {cells.length > 0 && <HeatGrid cells={cells} label={t("dash.perThread")} />}
    </Card>
  );
}

export function RamCard({ store, stale }: CardProps) {
  const t = useT();
  const total = val(store, "mem.total");
  const used = val(store, "mem.used") ?? 0, cache = val(store, "mem.cache") ?? 0, arc = val(store, "mem.arc"), free = val(store, "mem.free") ?? 0;
  const f = (n: number) => formatBytes(n, lang());
  const pct = total ? ((total - free) / total) * 100 : 0;
  const hist = store.series("mem.used"), tot = total ?? 1;
  return (
    <Card title={t("dash.ram")} subtitle={total ? `${t("dash.total")} ${f(total)}` : undefined} stale={stale}>
      {total === undefined ? <p className={styles.empty}>{t("dash.noData")}</p> : (
        <>
          <StackedBar total={total} format={f} segments={[
            { label: t("dash.ramUsed"), value: used, color: "var(--y-chart-1)" },
            ...(arc !== undefined ? [{ label: t("dash.ramArc"), value: arc, color: "var(--y-chart-5)" }] : []),
            { label: t("dash.ramCache"), value: cache, color: "var(--y-chart-3)" },
            { label: t("dash.ramFree"), value: free, color: "var(--y-surface-2)" },
          ]} />
          <LineChart lines={[{ values: hist.map((v) => (v / tot) * 100), color: "var(--y-chart-1)" }]} max={100} label={`${t("dash.ramUsed")} (${t("dash.last5m")})`} />
          <div className={styles.stats}>
            <Stat label={t("dash.ramUsed")} value={formatPercent(pct, 0, lang())} />
            {val(store, "mem.swap.used") !== undefined && <Stat label={t("dash.swap")} value={f(val(store, "mem.swap.used") ?? 0)} />}
          </div>
        </>
      )}
    </Card>
  );
}

export function NetCard({ store, units, stale, device }: CardProps & { device: MetricDevice }) {
  const t = useT();
  const id = device.id;
  const rx = val(store, `net.${id}.rx`), tx = val(store, `net.${id}.tx`), speed = val(store, `net.${id}.speed`) ?? (device.props?.speedBitsPerSec as number | undefined);
  const up = device.props?.up !== false;
  const usedPct = speed && rx !== undefined && tx !== undefined ? (Math.max(rx, tx) * 8 / speed) * 100 : undefined;
  return (
    <Card title={id} subtitle={speed ? `${t("dash.linkSpeed")} ${formatLinkSpeed(speed, lang())}` : up ? undefined : t("dash.linkDown")} stale={stale}>
      <div className={styles.stats}>
        <Stat label={`↓ ${t("dash.download")}`} value={fmt(rx, (v) => formatRate(v, units, lang()))} />
        <Stat label={`↑ ${t("dash.upload")}`} value={fmt(tx, (v) => formatRate(v, units, lang()))} />
        <Stat label={t("dash.errors")} value={fmt(val(store, `net.${id}.errors`), (v) => String(Math.round(v)))} />
      </div>
      <LineChart lines={[{ values: store.series(`net.${id}.rx`), color: "var(--y-chart-1)" }, { values: store.series(`net.${id}.tx`), color: "var(--y-chart-2)" }]} label={`${id} ${t("dash.download")}/${t("dash.upload")} (${t("dash.last5m")})`} />
      {usedPct !== undefined && <Bar pct={usedPct} label={t("dash.linkSpeed")} />}
    </Card>
  );
}

export function GpuCard({ store, temp, stale, device }: CardProps & { device: MetricDevice }) {
  const t = useT();
  const id = device.id;
  const util = val(store, `gpu.${id}.util`), used = val(store, `gpu.${id}.vram.used`), total = val(store, `gpu.${id}.vram.total`);
  const power = val(store, `gpu.${id}.power`), limit = val(store, `gpu.${id}.power.limit`);
  return (
    <Card title={`${t("dash.gpu")} ${id}`} subtitle={device.label} stale={stale}>
      <div className={styles.row}>
        <Gauge value={util ?? 0} label={t("dash.util")}>{fmt(util, (v) => `${Math.round(v)}%`)}</Gauge>
        <div className={styles.stats}>
          <Stat label={t("dash.temp")} value={fmt(val(store, `gpu.${id}.temp`), (v) => formatTemp(v, temp, lang()))} />
          <Stat label={t("dash.power")} value={power === undefined ? dash : `${formatWatts(power, lang())}${limit ? " " + t("dash.powerLimit", { limit: formatWatts(limit, lang()) }) : ""}`} />
          <Stat label={t("dash.fan")} value={fmt(val(store, `gpu.${id}.fan`), (v) => formatPercent(v, 0, lang()))} />
        </div>
      </div>
      <LineChart lines={[{ values: store.series(`gpu.${id}.util`), color: "var(--y-chart-5)" }]} max={100} label={`${t("dash.gpu")} ${id} ${t("dash.util")} (${t("dash.last5m")})`} />
      {used !== undefined && total ? (
        <div>
          <div className={styles.diskStats}><span><em>{t("dash.vram")}</em>{formatBytes(used, lang())} / {formatBytes(total, lang())}</span></div>
          <Bar pct={(used / total) * 100} label={t("dash.vram")} />
        </div>
      ) : null}
    </Card>
  );
}

export function DisksCard({ store, units, temp, stale, devices }: CardProps & { devices: MetricDevice[] }) {
  const t = useT();
  return (
    <Card title={t("dash.disks")} stale={stale} className={styles.wide}>
      {devices.length === 0 ? <p className={styles.empty}>{t("dash.noDisks")}</p> : (
        <ul className={styles.disks}>
          {devices.map((d) => {
            const id = d.id, cap = d.props?.capacityBytes as number | undefined, kind = String(d.props?.type ?? "");
            const busy = val(store, `disk.${id}.util`), temperature = val(store, `disk.${id}.temp`);
            return (
              <li key={id} className={styles.disk}>
                <div>
                  <h3>{d.label}<span className={styles.badge}>{kind}</span></h3>
                  <small>{id}{cap ? ` · ${formatCapacity(cap, lang())}` : ""}</small>
                </div>
                <div>
                  <div className={styles.diskStats}>
                    <span><em>{t("dash.read")}</em>{fmt(val(store, `disk.${id}.read`), (v) => formatRate(v, units, lang()))}</span>
                    <span><em>{t("dash.write")}</em>{fmt(val(store, `disk.${id}.write`), (v) => formatRate(v, units, lang()))}</span>
                    <span>{fmt(val(store, `disk.${id}.iops`), (v) => formatIops(v, lang()))}</span>
                    {temperature !== undefined && <span>{formatTemp(temperature, temp, lang())}</span>}
                  </div>
                  <Bar pct={busy ?? 0} label={`${t("dash.busy")} ${id}`} />
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </Card>
  );
}

export function SystemCard({ store, stale }: CardProps) {
  const t = useT();
  return (
    <Card title={t("dash.system")} stale={stale} className={styles.wide}>
      <div className={styles.stats}>
        {(["load1", "load5", "load15"] as const).map((k, i) => <Stat key={k} label={`${t("dash.load")} ${[1, 5, 15][i]} min`} value={fmt(val(store, `sys.${k}`), (v) => v.toFixed(2))} />)}
        <Stat label={t("dash.uptime")} value={fmt(val(store, "sys.uptime"), formatUptime)} />
      </div>
    </Card>
  );
}
