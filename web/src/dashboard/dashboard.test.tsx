import { act, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { DashboardPage } from "../pages/DashboardPage";
import { jsonRes, routeFetch } from "../test/fetchMock";
import { formatBytes, formatCapacity, formatRate, formatTemp, formatUptime } from "./format";
import { MetricsStore } from "./store";

describe("format", () => {
  it("formats sizes, rates and temperatures", () => {
    expect(formatBytes(64 * 1024 ** 3, "en")).toBe("64.0 GiB");
    expect(formatCapacity(14_000_519_643_136, "en")).toBe("14.0 TB");
    expect(formatRate(125_000_000, "bytes", "en")).toBe("125 MB/s");
    expect(formatRate(125_000_000, "bits", "en")).toBe("1.00 Gbit/s");
    expect(formatTemp(60, "c", "en")).toBe("60 °C");
    expect(formatTemp(60, "f", "en")).toBe("140 °F");
    expect(formatUptime(3 * 86400 + 3600)).toBe("3d 1h");
    expect(formatBytes(NaN)).toBe("–");
  });
});

describe("MetricsStore", () => {
  it("keeps the latest value and a bounded history, and notifies subscribers", () => {
    const s = new MetricsStore();
    let calls = 0;
    s.subscribe(() => calls++);
    for (let i = 0; i < 400; i++) s.push({ t: i, v: { "a": i, "cpu.core.0.util": i } });
    expect(s.get("a")).toBe(399);
    expect(s.series("a")).toHaveLength(300);
    expect(s.series("cpu.core.0.util")).toHaveLength(60); // per-core series keep a short tail
    expect(calls).toBe(400);
    expect(s.series("missing")).toEqual([]);
  });

  it("seeds history underneath live points", () => {
    const s = new MetricsStore();
    s.push({ t: 1, v: { a: 10 } });
    s.seed({ a: [1, 2, 3] });
    expect(s.series("a")).toEqual([1, 2, 3, 10]);
    expect(s.get("a")).toBe(10);
  });
});

class FakeWS {
  static all: FakeWS[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  constructor(public url: string) { FakeWS.all.push(this); }
  close() { this.closed = true; }
  open() { this.onopen?.(); }
  send(frame: object) { this.onmessage?.({ data: JSON.stringify(frame) }); }
  drop() { this.onclose?.(); }
}

const CATALOG = {
  series: [],
  devices: [
    { kind: "cpu", id: "0", label: "Xeon Silver 4314", props: { cores: 2, threads: 4, logical: [0, 1, 2, 3] } },
    { kind: "net", id: "eth0", label: "eth0", props: { up: true, speedBitsPerSec: 10_000_000_000 } },
    { kind: "net", id: "docker0", label: "docker0", hidden: true, props: { up: true } },
    { kind: "gpu", id: "0", label: "NVIDIA RTX 3090", props: {} },
    { kind: "disk", id: "sda", label: "WDC Red 14TB", props: { type: "hdd", capacityBytes: 14_000_519_643_136 } },
  ],
  providers: [{ name: "rapl", ok: false, reason: "RAPL requires the privileged helper" }],
};

function renderDash() {
  FakeWS.all = [];
  routeFetch({
    "GET /metrics/catalog": jsonRes(CATALOG),
    "GET /metrics/history": jsonRes({ range: "5m", stepSeconds: 0, series: { "cpu.0.util": { t: [1, 2], v: [10, 20] } } }),
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}><DashboardPage live={{ socketFactory: (u) => new FakeWS(u) as unknown as WebSocket }} /></QueryClientProvider>);
}

const FRAME = {
  t: 1000,
  v: {
    "cpu.0.util": 42, "cpu.0.temp": 61.4, "cpu.0.freq": 2.4e9, "cpu.core.0.util": 90, "cpu.core.1.util": 10, "cpu.core.2.util": 50, "cpu.core.3.util": 0,
    "mem.total": 64 * 1024 ** 3, "mem.used": 12 * 1024 ** 3, "mem.cache": 6 * 1024 ** 3, "mem.arc": 20 * 1024 ** 3, "mem.free": 26 * 1024 ** 3,
    "net.eth0.rx": 125_000_000, "net.eth0.tx": 5_000_000, "net.eth0.errors": 0, "net.eth0.speed": 10e9,
    "gpu.0.util": 77, "gpu.0.temp": 66, "gpu.0.vram.used": 8 * 1024 ** 3, "gpu.0.vram.total": 24 * 1024 ** 3, "gpu.0.power": 210, "gpu.0.power.limit": 350, "gpu.0.fan": 55,
    "disk.sda.read": 80_000_000, "disk.sda.write": 1_000_000, "disk.sda.iops": 150, "disk.sda.util": 35, "disk.sda.temp": 36,
    "sys.load1": 1.5, "sys.load5": 2, "sys.load15": 2.5, "sys.uptime": 259200,
  },
};

describe("Dashboard", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("shows every card with live values and the connection state", async () => {
    renderDash();
    expect(await screen.findByText("Xeon Silver 4314 · 2 cores · 4 threads")).toBeInTheDocument();
    await waitFor(() => expect(FakeWS.all).toHaveLength(1));
    expect(FakeWS.all[0]!.url).toMatch(/^wss?:\/\/.+\/ws\/metrics$/);
    act(() => { FakeWS.all[0]!.open(); FakeWS.all[0]!.send(FRAME); });

    expect(await screen.findByText("Live")).toBeInTheDocument();
    expect(screen.getByText("61 °C")).toBeInTheDocument();           // CPU temperature
    expect(screen.getByText("2.40 GHz")).toBeInTheDocument();
    expect(screen.getByText("125 MB/s")).toBeInTheDocument();         // eth0 download
    expect(screen.getByText("RTX 3090", { exact: false })).toBeInTheDocument();
    expect(screen.getByText("210 W of 350 W")).toBeInTheDocument();
    expect(screen.getByText("80.0 MB/s")).toBeInTheDocument(); // disk read
    expect(screen.getByRole("img", { name: "Load per thread" }).children).toHaveLength(4);
    expect(screen.getByText("3d 0h")).toBeInTheDocument();
    // the ARC is separated from the page cache
    expect(screen.getByText("ZFS ARC")).toBeInTheDocument();
    // provider problems are explained, not hidden
    expect(screen.getByText(/RAPL requires the privileged helper/)).toBeInTheDocument();
  });

  it("hides virtual interfaces until asked and survives a dropped socket", async () => {
    renderDash();
    await screen.findByText("eth0");
    expect(screen.queryByText("docker0")).not.toBeInTheDocument();
    act(() => { screen.getByLabelText("Show virtual interfaces").click(); });
    expect(await screen.findByText("docker0")).toBeInTheDocument();

    await waitFor(() => expect(FakeWS.all).toHaveLength(1));
    act(() => { FakeWS.all[0]!.open(); });
    expect(await screen.findByText("Live")).toBeInTheDocument();
    act(() => { FakeWS.all[0]!.drop(); });
    expect(await screen.findByText("Reconnecting…")).toBeInTheDocument();
  });
});
