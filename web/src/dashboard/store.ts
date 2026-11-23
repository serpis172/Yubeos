import { useSyncExternalStore } from "react";

/** Samples kept per series for the live charts (5 minutes at 1 Hz). Per-core series keep a short tail only. */
export const HISTORY = 300;
const CORE_HISTORY = 60;

export interface Frame { t: number; v: Record<string, number> }
export type Connection = "connecting" | "live" | "reconnecting" | "polling";

/**
 * Client-side mirror of the server's series: latest values, a short history for charts, a monotonic version for
 * cheap change detection. Fed by the WebSocket (or polling) and by one-off history prefills.
 */
export class MetricsStore {
  private values = new Map<string, number>();
  private history = new Map<string, number[]>();
  private listeners = new Set<() => void>();
  private _version = 0;
  private _conn: Connection = "connecting";
  private _lastT = 0;

  get version() { return this._version; }
  get connection() { return this._conn; }
  get lastFrameAt() { return this._lastT; }

  subscribe = (cb: () => void) => { this.listeners.add(cb); return () => this.listeners.delete(cb); };
  getVersion = () => this._version;

  private bump() { this._version++; this.listeners.forEach((l) => l()); }

  setConnection(c: Connection) {
    if (c === this._conn) return;
    this._conn = c;
    this.bump();
  }

  push(f: Frame) {
    this._lastT = f.t;
    for (const [k, v] of Object.entries(f.v)) {
      this.values.set(k, v);
      let h = this.history.get(k);
      if (!h) this.history.set(k, (h = []));
      h.push(v);
      const cap = k.startsWith("cpu.core.") ? CORE_HISTORY : HISTORY;
      if (h.length > cap) h.splice(0, h.length - cap);
    }
    this.bump();
  }

  /** Replace the history of series (prefill from /metrics/history). Live points already received are appended after it. */
  seed(series: Record<string, number[]>) {
    for (const [k, v] of Object.entries(series)) {
      const cap = k.startsWith("cpu.core.") ? CORE_HISTORY : HISTORY;
      const live = this.history.get(k) ?? [];
      const merged = [...v, ...live].slice(-cap);
      this.history.set(k, merged);
      if (!this.values.has(k) && merged.length) this.values.set(k, merged[merged.length - 1]!);
    }
    this.bump();
  }

  get(key: string): number | undefined { return this.values.get(key); }
  series(key: string): number[] { return this.history.get(key) ?? EMPTY; }
  has(key: string): boolean { return this.values.has(key); }
}

const EMPTY: number[] = [];

/** Re-render the calling component whenever the store changes (≈1 Hz). */
export function useStoreVersion(store: MetricsStore): number {
  return useSyncExternalStore(store.subscribe, store.getVersion);
}
