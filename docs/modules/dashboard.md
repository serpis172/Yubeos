# Module: Dashboard

Route `/` · Roles: user, admin · Backend: `internal/metrics/*` · API: [`/metrics/*`, `/ws/metrics`](../API.md#metrics)

Derived from the hand-drawn sketch: CPU cards (one per socket) with a per-core heat grid, RAM gauge + history, network cards, GPU strip (core / temp / VRAM / power), disk widgets.

## 1. Widget catalogue

| Widget | Content | Update |
|---|---|---|
| **CPU card** (one per physical package; sketch "CPU 1/2/3") | Model name, `TOT n CORE` (cores/threads), utilisation %, temperature °C, package power W, line chart (last 5 min default) | 1 s |
| **CPU core grid** | Heat-map cells, one per logical CPU, colour = utilisation; hover = freq, temp (if per-core hwmon exists) | 1 s |
| **RAM** | Gauge `used / total` (e.g. 1/64 GB), stacked breakdown **Free · Cache/Buffers · ZFS ARC · Used**, history chart | 1 s |
| **Network** (sketch "RETE") | One card per interface (hidden: `lo`, `veth*`, `docker*`, `br-*` unless toggled): ↓/↑ throughput (e.g. `67 MB/s`), link speed (e.g. `1 GbE`), errors/drops, sparkline | 1 s |
| **GPU** (sketch "GPU 1/2") | Name, core util %, temperature, VRAM used/total, power W / limit, fan %; per-GPU sparkline | 1 s |
| **Disk** (sketch "Disk 1/2") | Alias (from Disk Manager), type, capacity/used, read/write MB/s, IOPS, temperature, SMART status badge | 2 s |
| **System bar** (top) | Hostname, uptime, load avg, kernel, pending alerts | 10 s |

Layout: responsive CSS grid of cards; v1 fixed order matching the sketch, user-resizable/reorderable grid (`react-grid-layout`-style, persisted per user) in Phase 2b.

## 2. Series namespace

Flat dotted keys; each series has `{unit, label, group}` metadata served by `GET /metrics/catalog`.

```
cpu.<pkg>.util  cpu.<pkg>.temp  cpu.<pkg>.power  cpu.<pkg>.freq
cpu.core.<n>.util
mem.used  mem.free  mem.cache  mem.arc  mem.total  mem.swap.used
net.<if>.rx  net.<if>.tx  net.<if>.speed  net.<if>.errors
gpu.<n>.util  gpu.<n>.temp  gpu.<n>.vram.used  gpu.<n>.vram.total  gpu.<n>.power  gpu.<n>.fan
disk.<id>.read  disk.<id>.write  disk.<id>.iops  disk.<id>.temp  disk.<id>.util
sys.load1  sys.load5  sys.load15
```

## 3. Data sources (Linux providers)

| Metric | Source | Notes |
|---|---|---|
| CPU util | `/proc/stat` deltas | per logical CPU; package = weighted by topology `physical_package_id` |
| CPU topology/model | `/sys/devices/system/cpu/cpu*/topology/*`, `/proc/cpuinfo` | multi-socket → multiple cards |
| CPU freq | `/sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq` | optional |
| CPU temp | hwmon: `coretemp` (`Package id N`), `k10temp` (`Tctl`/`Tdie`) | match hwmon ↔ package by label |
| CPU power | RAPL `/sys/class/powercap/intel-rapl:*/energy_uj` (Intel/AMD Zen) | **root-only since 2020** (side-channel mitigation) ⇒ read via `rapl.read` in `yubeos-priv`; counter wrap handled with `max_energy_range_uj`; absent ⇒ widget hides power |
| RAM | `/proc/meminfo`; ZFS ARC `/proc/spl/kstat/zfs/arcstats` (`size`) | ARC reported separately because ZFS caches outside page cache; "available" excludes reclaimable ARC conservatively |
| Network | `/proc/net/dev`; speed `/sys/class/net/<if>/speed` | rate = Δbytes/Δt; counter wrap handled |
| NVIDIA GPU | one long-lived `nvidia-smi --query-gpu=index,name,utilization.gpu,temperature.gpu,memory.used,memory.total,power.draw,power.limit,fan.speed --format=csv,noheader,nounits -l 1` | single process, restarted with back-off if it dies |
| AMD GPU | `/sys/class/drm/card*/device/{gpu_busy_percent,mem_info_vram_used,mem_info_vram_total}`, hwmon `temp1_input`, `power1_average` | v1.1 |
| Intel GPU | `intel_gpu_top -J` | v1.2 |
| Disk IO | `/proc/diskstats` deltas | |
| Disk temp / SMART | `smartctl -j -A -H` via `smart.read` (cached 60 s) or hwmon `drivetemp` | `drivetemp` module is preferred for temperature (no root, cheap) |

Every provider implements `Devices()` + `Sample()`; missing hardware yields an empty device list and the widget simply does not render.

## 4. Behaviour details

- **Sampling:** one goroutine per provider, fixed 1 s tick (`time.Ticker`, drift-corrected using monotonic clock deltas — rates are computed from *actual* elapsed time, not the nominal 1 s).
- **Ring buffer:** fixed-size array per series (3 600 × `float32` + timestamps), lock-free reads via copy-on-snapshot; ~30 KB per series.
- **Staleness:** a series not updated for > 5 s is flagged `stale` and drawn dimmed.
- **Alerts (v1.1):** threshold rules (temperature, SMART, pool health, disk fullness) → toast + top-bar badge; no external notification channels in 1.0.
- **Units:** SI in the API (bytes, bytes/s, °C, W); formatting is a UI concern (user preference: MB/s vs Mbit/s, °C vs °F).

## 5. UI states

Loading skeleton · Live · Reconnecting (frozen charts + banner) · Provider unavailable (card shows reason, e.g. "RAPL requires privileged helper") · Demo mode (ribbon).

## 6. Acceptance criteria (Phase 2)

1. With `--demo`, all widgets render with deterministic data for a given seed.
2. On real hardware, values match `htop`, `nvidia-smi`, `ip -s link` within ±2 %.
3. 30 min soak with 3 tabs open: daemon CPU < 1 %, RSS growth < 5 MB.
4. Unplugging a NIC / killing `nvidia-smi` degrades one card only, with recovery without restart.
5. Multi-socket and no-GPU / no-RAPL hosts covered by fixtures.
