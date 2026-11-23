import { type CSSProperties, type ReactNode } from "react";
import styles from "./Dashboard.module.css";

export interface Line { values: number[]; color: string; label?: string }

const W = 300;

/** Responsive SVG line/area chart; all lines share one scale. `max` pins the top (percent charts use 100). */
export function LineChart({ lines, height = 64, max, fill = true, label }: { lines: Line[]; height?: number; max?: number; fill?: boolean; label: string }) {
  const n = Math.max(...lines.map((l) => l.values.length), 0);
  const top = max ?? Math.max(1e-9, ...lines.flatMap((l) => l.values)) * 1.15;
  const x = (i: number, len: number) => (len <= 1 ? W : (i / (len - 1)) * W);
  const y = (v: number) => height - Math.max(0, Math.min(1, v / top)) * (height - 2) - 1;
  return (
    <svg className={styles.chart} viewBox={`0 0 ${W} ${height}`} preserveAspectRatio="none" role="img" aria-label={label} style={{ height }}>
      <line className={styles.gridline} x1="0" x2={W} y1={y(top / 2)} y2={y(top / 2)} />
      {n < 2 && <text className={styles.chartEmpty} x={W / 2} y={height / 2} textAnchor="middle">…</text>}
      {lines.map((l, k) => {
        if (l.values.length < 2) return null;
        const pts = l.values.map((v, i) => `${x(i, l.values.length).toFixed(1)},${y(v).toFixed(1)}`);
        return (
          <g key={k}>
            {fill && <polygon points={`0,${height} ${pts.join(" ")} ${W},${height}`} fill={l.color} opacity="0.16" />}
            <polyline points={pts.join(" ")} fill="none" stroke={l.color} strokeWidth="1.6" vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
          </g>
        );
      })}
    </svg>
  );
}

export const heatColor = (pct: number) => (pct >= 85 ? "var(--y-danger)" : pct >= 60 ? "var(--y-warning)" : "var(--y-accent)");

/** Circular gauge; the arc turns warning/danger as the value rises. */
export function Gauge({ value, max = 100, size = 96, children, label }: { value: number; max?: number; size?: number; children?: ReactNode; label: string }) {
  const r = 40, c = 2 * Math.PI * r;
  const f = Math.max(0, Math.min(1, value / max));
  return (
    <div className={styles.gauge} style={{ width: size, height: size }} role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={max} aria-valuenow={Math.round(value)}>
      <svg viewBox="0 0 100 100" aria-hidden="true">
        <circle cx="50" cy="50" r={r} className={styles.gaugeTrack} />
        <circle cx="50" cy="50" r={r} className={styles.gaugeArc} stroke={heatColor((f * 100))} strokeDasharray={`${f * c} ${c}`} transform="rotate(-90 50 50)" />
      </svg>
      <div className={styles.gaugeText}>{children}</div>
    </div>
  );
}

export interface Segment { label: string; value: number; color: string }

/** Horizontal stacked bar with a legend; `total` is the full width. */
export function StackedBar({ segments, total, format }: { segments: Segment[]; total: number; format: (n: number) => string }) {
  return (
    <div>
      <div className={styles.stack} role="img" aria-label={segments.map((s) => `${s.label} ${format(s.value)}`).join(", ")}>
        {segments.map((s) => <span key={s.label} style={{ width: `${total > 0 ? (s.value / total) * 100 : 0}%`, background: s.color }} />)}
      </div>
      <ul className={styles.legend}>
        {segments.map((s) => (
          <li key={s.label}><i style={{ background: s.color }} />{s.label}<b>{format(s.value)}</b></li>
        ))}
      </ul>
    </div>
  );
}

/** Thin horizontal progress bar. */
export function Bar({ pct, label }: { pct: number; label: string }) {
  const p = Math.max(0, Math.min(100, pct));
  return <div className={styles.bar} role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(p)}><span style={{ width: `${p}%`, background: heatColor(p) }} /></div>;
}

/** One cell per logical CPU, coloured by load. */
export function HeatGrid({ cells, label }: { cells: { id: number; pct: number }[]; label: string }) {
  return (
    <div className={styles.heat} role="img" aria-label={label}>
      {cells.map((c) => (
        <span key={c.id} title={`CPU ${c.id}: ${Math.round(c.pct)} %`}
              style={{ "--heat": heatColor(c.pct), "--mix": `${Math.round(18 + c.pct * 0.8)}%` } as CSSProperties} />
      ))}
    </div>
  );
}

export function Stat({ label, value, hint }: { label: string; value: ReactNode; hint?: string }) {
  return <div className={styles.stat} title={hint}><span>{label}</span><b>{value}</b></div>;
}

export function Card({ title, subtitle, right, stale, children, className }: { title: string; subtitle?: string; right?: ReactNode; stale?: boolean; children: ReactNode; className?: string }) {
  return (
    <section className={`${styles.card} ${stale ? styles.stale : ""} ${className ?? ""}`} aria-label={title}>
      <header>
        <div><h2>{title}</h2>{subtitle && <p>{subtitle}</p>}</div>
        {right}
      </header>
      {children}
    </section>
  );
}
