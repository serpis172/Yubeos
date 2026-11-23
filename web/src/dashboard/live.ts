import { useEffect, useMemo, useRef } from "react";
import { getMetricHistory, getMetricSnapshot } from "../lib/api";
import { MetricsStore, type Frame } from "./store";

const BACKOFF_MIN = 1000;
const BACKOFF_MAX = 15000;
const POLL_AFTER_FAILURES = 3;
const POLL_MS = 2000;

export interface LiveOptions {
  /** Test hook: how to build the WebSocket (default: same-origin /ws/metrics). */
  socketFactory?: (url: string) => WebSocket;
}

function wsUrl(): string {
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${window.location.host}/ws/metrics`;
}

/**
 * Feeds a MetricsStore: history prefill, then the WebSocket with exponential back-off; after three failed attempts
 * it degrades to polling /metrics/snapshot every 2 s (and keeps retrying the socket in between).
 */
export function useLiveMetrics(opts: LiveOptions = {}): MetricsStore {
  const store = useMemo(() => new MetricsStore(), []);
  const factory = useRef(opts.socketFactory);
  factory.current = opts.socketFactory;

  useEffect(() => {
    let closed = false;
    let ws: WebSocket | undefined;
    let retry: number | undefined;
    let poll: number | undefined;
    let failures = 0;
    let backoff = BACKOFF_MIN;
    const ac = new AbortController();

    void getMetricHistory("cpu.*,mem.*,net.*,gpu.*,disk.*,sys.*", "5m", ac.signal).then((h) => {
      store.seed(Object.fromEntries(Object.entries(h.series).map(([k, p]) => [k, p.v])));
    }).catch(() => { /* live data still works without the prefill */ });

    const startPolling = () => {
      if (poll !== undefined) return;
      store.setConnection("polling");
      const tick = () => void getMetricSnapshot(ac.signal).then((s) => store.push({ t: s.t, v: s.values })).catch(() => {});
      tick();
      poll = window.setInterval(tick, POLL_MS);
    };
    const stopPolling = () => { if (poll !== undefined) { window.clearInterval(poll); poll = undefined; } };

    const connect = () => {
      if (closed) return;
      store.setConnection(poll === undefined ? (failures === 0 ? "connecting" : "reconnecting") : "polling");
      try {
        ws = factory.current ? factory.current(wsUrl()) : new WebSocket(wsUrl());
      } catch {
        fail();
        return;
      }
      ws.onopen = () => { failures = 0; backoff = BACKOFF_MIN; stopPolling(); store.setConnection("live"); };
      ws.onmessage = (e) => {
        try {
          const m = JSON.parse(String(e.data)) as Partial<Frame>;
          if (m.v && typeof m.t === "number") store.push(m as Frame);
        } catch { /* ignore malformed frames */ }
      };
      ws.onclose = () => { if (!closed) fail(); };
      ws.onerror = () => { /* onclose follows */ };
    };

    const fail = () => {
      failures++;
      if (failures >= POLL_AFTER_FAILURES) startPolling(); else store.setConnection("reconnecting");
      retry = window.setTimeout(connect, backoff);
      backoff = Math.min(backoff * 2, BACKOFF_MAX);
    };

    connect();
    return () => { closed = true; ac.abort(); window.clearTimeout(retry); stopPolling(); ws?.close(); };
  }, [store]);

  return store;
}
