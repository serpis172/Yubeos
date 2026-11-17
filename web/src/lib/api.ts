import type { components } from "./api.gen";

export type SystemInfo = components["schemas"]["SystemInfo"];
export type Problem = components["schemas"]["Problem"];
export type Me = components["schemas"]["Me"];

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
  const { signal, timeoutMs = DEFAULT_TIMEOUT_MS, ...rest } = init;
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

export const login = (username: string, password: string) => api<Me>("/auth/login", json({ username, password }));
export const logout = () => api<void>("/auth/logout", { method: "POST" });
export const setupPassword = (token: string, newPassword: string) => api<void>("/auth/setup", json({ token, newPassword }));
