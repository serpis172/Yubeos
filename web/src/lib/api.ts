import type { components } from "./api.gen";

export type SystemInfo = components["schemas"]["SystemInfo"];
export type Problem = components["schemas"]["Problem"];
export type Me = components["schemas"]["Me"];
export type Factors = components["schemas"]["Factors"];
export type Passkey = components["schemas"]["Passkey"];
export type Device = components["schemas"]["Device"];
export type RemoteStatus = components["schemas"]["RemoteStatus"];
export type Preferences = components["schemas"]["Preferences"];
export type MetricCatalog = components["schemas"]["MetricCatalog"];
export type MetricSnapshot = components["schemas"]["MetricSnapshot"];
export type MetricHistory = components["schemas"]["MetricHistory"];
export type MetricDevice = components["schemas"]["MetricDevice"];
export type MetricSeries = components["schemas"]["MetricSeries"];
export type WebAuthnChallenge = components["schemas"]["WebAuthnChallenge"];
export type SecondFactorChallenge = components["schemas"]["SecondFactorChallenge"];

/** Error carrying an RFC 9457 problem document (or a synthesized one for network/timeout failures). */
export class ApiError extends Error {
  constructor(public readonly problem: Problem) {
    super(problem.detail || problem.title);
    this.name = "ApiError";
  }
  get status(): number {
    return this.problem.status;
  }
}

export const DEFAULT_TIMEOUT_MS = 15_000;

let csrfToken = "";
/** Set after login/`/auth/me` (Phase 1). Sent on every non-GET request. */
export function setCsrfToken(token: string): void {
  csrfToken = token;
}

export interface ApiOptions extends Omit<RequestInit, "signal"> {
  /** Receives the HTTP status of a successful response (used where 200 and 202 differ). */
  onStatus?: (status: number) => void;
  /** Caller cancellation (e.g. React Query). Combined with the timeout. */
  signal?: AbortSignal;
  /** Milliseconds before the request is aborted; 0 disables. */
  timeoutMs?: number;
}

function networkProblem(title: string, detail: string, status: number): Problem {
  return { type: "about:blank", title, status, detail };
}

/** AbortSignal.timeout with a fallback for environments that lack it (older browsers, jsdom). */
function timeoutSignal(ms: number): AbortSignal {
  if (typeof AbortSignal.timeout === "function") return AbortSignal.timeout(ms);
  const ac = new AbortController();
  setTimeout(() => ac.abort(new DOMException("Timed out", "TimeoutError")), ms);
  return ac.signal;
}

/** AbortSignal.any with a fallback. */
function anySignal(signals: AbortSignal[]): AbortSignal {
  if (signals.length === 1) return signals[0]!;
  if (typeof AbortSignal.any === "function") return AbortSignal.any(signals);
  const ac = new AbortController();
  for (const s of signals) {
    if (s.aborted) { ac.abort(s.reason); break; }
    s.addEventListener("abort", () => ac.abort(s.reason), { once: true });
  }
  return ac.signal;
}

export async function api<T>(path: string, init: ApiOptions = {}): Promise<T> {
  const { signal, timeoutMs = DEFAULT_TIMEOUT_MS, onStatus, ...rest } = init;
  const method = (rest.method ?? "GET").toUpperCase();
  const headers = new Headers(rest.headers);
  headers.set("Accept", "application/json");
  if (method !== "GET" && method !== "HEAD" && csrfToken) headers.set("X-Yubeos-CSRF", csrfToken);

  const signals: AbortSignal[] = [];
  if (signal) signals.push(signal);
  if (timeoutMs > 0) signals.push(timeoutSignal(timeoutMs));
  const combined = signals.length > 0 ? anySignal(signals) : undefined;

  let res: Response;
  try {
    res = await fetch(`/api/v1${path}`, { ...rest, headers, credentials: "same-origin", signal: combined });
  } catch (e) {
    if (signal?.aborted) throw e; // caller cancelled: let it propagate untouched
    const timedOut = e instanceof DOMException && e.name === "TimeoutError";
    throw new ApiError(
      networkProblem(timedOut ? "Request timed out" : "Network error", timedOut ? `No response within ${timeoutMs} ms` : "The server could not be reached", 0),
    );
  }

  if (!res.ok) {
    let problem: Problem;
    try {
      problem = (await res.json()) as Problem;
    } catch {
      problem = networkProblem(res.statusText || "Request failed", "", res.status);
    }
    throw new ApiError(problem);
  }
  onStatus?.(res.status);
  if (res.status === 204 || res.status === 205 || res.headers.get("Content-Length") === "0") return undefined as T;
  if (!(res.headers.get("Content-Type") ?? "").includes("json")) return undefined as T;
  return (await res.json()) as T;
}

export const getSystemInfo = (signal?: AbortSignal) => api<SystemInfo>("/system/info", { signal });

export const getMe = (signal?: AbortSignal) => api<Me>("/auth/me", { signal });

const json = (body: unknown): Pick<RequestInit, "method" | "body" | "headers"> => ({
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(body),
});

/** 200 = session created; 202 = password accepted, a second factor is needed (see SecondFactorChallenge). */
export type LoginOutcome = { me: Me; challenge?: undefined } | { me?: undefined; challenge: SecondFactorChallenge };

export async function login(username: string, password: string): Promise<LoginOutcome> {
  const res = await apiRaw<Me | SecondFactorChallenge>("/auth/login", json({ username, password }));
  return res.status === 202 ? { challenge: res.body as SecondFactorChallenge } : { me: res.body as Me };
}

/** Like api() but also exposes the status code (login answers 200 or 202). */
async function apiRaw<T>(path: string, init: ApiOptions): Promise<{ status: number; body: T }> {
  let status = 200;
  const body = await api<T>(path, { ...init, onStatus: (s: number) => { status = s; } } as ApiOptions);
  return { status, body };
}

export const logout = () => api<void>("/auth/logout", { method: "POST" });
export const setupPassword = (token: string, newPassword: string) => api<void>("/auth/setup", json({ token, newPassword }));
export const changePassword = (currentPassword: string, newPassword: string) =>
  api<void>("/auth/password", { ...json({ currentPassword, newPassword }), method: "PUT" });

// ---- second factors ---------------------------------------------------------------------------------------------
export const verifySecondFactor = (b: { challengeId: string; method: "totp" | "recovery"; code: string; trustDevice: boolean }) =>
  api<Me>("/auth/2fa/verify", json(b));
export const webauthnLoginBegin = (challengeId?: string) =>
  api<WebAuthnChallenge>("/auth/webauthn/login/begin", challengeId ? json({ challengeId }) : { method: "POST" });
export const webauthnLoginFinish = (b: { challengeId: string; trustDevice: boolean; credential: unknown }) =>
  api<Me>("/auth/webauthn/login/finish", json(b));

export const getFactors = (signal?: AbortSignal) => api<Factors>("/auth/factors", { signal });
export const totpEnroll = () => api<{ secret: string; otpauthUri: string }>("/auth/totp/enroll", { method: "POST" });
export const totpConfirm = (code: string) => api<{ codes: string[] }>("/auth/totp/confirm", json({ code }));
export const totpRemove = () => api<void>("/auth/totp", { method: "DELETE" });
export const passkeyRegisterBegin = () => api<WebAuthnChallenge>("/auth/webauthn/register/begin", { method: "POST" });
export const passkeyRegisterFinish = (b: { challengeId: string; label: string; credential: unknown }) =>
  api<Passkey>("/auth/webauthn/register/finish", json(b));
export const passkeyRemove = (id: string) => api<void>(`/auth/webauthn/${encodeURIComponent(id)}`, { method: "DELETE" });
export const regenerateRecoveryCodes = () => api<{ codes: string[] }>("/auth/recovery-codes:regenerate", { method: "POST" });

export const stepUpBegin = () => api<WebAuthnChallenge>("/auth/stepup/begin", { method: "POST" });
export const stepUp = (b: { challengeId: string; method: "totp" | "recovery" | "webauthn" | "password"; code?: string; password?: string; credential?: unknown }) =>
  api<void>("/auth/stepup", json(b));

export const listDevices = (signal?: AbortSignal) => api<{ items: Device[] }>("/auth/devices", { signal });
export const revokeDevice = (id: string) => api<void>(`/auth/devices/${encodeURIComponent(id)}`, { method: "DELETE" });
export const revokeAllDevices = () => api<void>("/auth/devices:revokeAll", { method: "POST" });

export const getRemoteStatus = (signal?: AbortSignal) => api<RemoteStatus>("/security/remote-status", { signal });
export const setLockdown = (enabled: boolean) => api<void>("/security/lockdown", json({ enabled }));

// ---- preferences and metrics -------------------------------------------------------------------------------------
export const getPreferences = (signal?: AbortSignal) => api<Preferences>("/me/preferences", { signal });
export const putPreferences = (p: Preferences) => api<Preferences>("/me/preferences", { ...json(p), method: "PUT" });

export const getMetricCatalog = (signal?: AbortSignal) => api<MetricCatalog>("/metrics/catalog", { signal });
export const getMetricSnapshot = (signal?: AbortSignal) => api<MetricSnapshot>("/metrics/snapshot", { signal });
export const getMetricHistory = (series: string, range: "5m" | "1h" | "24h" | "7d" | "30d", signal?: AbortSignal) =>
  api<MetricHistory>(`/metrics/history?series=${encodeURIComponent(series)}&range=${range}`, { signal });
