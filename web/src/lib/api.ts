import type { components } from "./api.gen";

export type SystemInfo = components["schemas"]["SystemInfo"];
export type Problem = components["schemas"]["Problem"];

/** Error carrying an RFC 9457 problem document. */
export class ApiError extends Error {
  constructor(public readonly problem: Problem) {
    super(problem.detail || problem.title);
    this.name = "ApiError";
  }
}

let csrfToken = "";
/** Set after login/`/auth/me` (Phase 1). Sent on every non-GET request. */
export function setCsrfToken(token: string): void {
  csrfToken = token;
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method ?? "GET").toUpperCase();
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (method !== "GET" && method !== "HEAD" && csrfToken) headers.set("X-Yubeos-CSRF", csrfToken);

  const res = await fetch(`/api/v1${path}`, { ...init, headers, credentials: "same-origin" });
  if (!res.ok) {
    let problem: Problem;
    try {
      problem = (await res.json()) as Problem;
    } catch {
      problem = { type: "about:blank", title: res.statusText || "Request failed", status: res.status };
    }
    throw new ApiError(problem);
  }
  return (await res.json()) as T;
}

export const getSystemInfo = () => api<SystemInfo>("/system/info");
