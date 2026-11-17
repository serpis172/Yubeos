import { vi } from "vitest";

type Handler = (init: RequestInit | undefined, url: string) => Response | Promise<Response>;

/** Routes fetch by "METHOD /path" (path without the /api/v1 prefix). Unknown routes answer 404 problem+json. */
export function routeFetch(routes: Record<string, Handler | Response>) {
  const calls: { key: string; init?: RequestInit }[] = [];
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input).replace(/^.*\/api\/v1/, "");
    const key = `${(init?.method ?? "GET").toUpperCase()} ${url}`;
    calls.push({ key, init });
    const h = routes[key];
    if (!h) return problem(404, "not-found");
    if (h instanceof Response) return h.clone();
    return h(init, url);
  });
  vi.stubGlobal("fetch", fn);
  return { fn, calls };
}

export const jsonRes = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

export const problem = (status: number, code: string, extra: Record<string, unknown> = {}) =>
  new Response(JSON.stringify({ type: `https://yubeos.dev/problems/${code}`, title: code, status, ...extra }), {
    status, headers: { "Content-Type": "application/problem+json" },
  });

export const ME = { username: "bob", role: "user", csrf: "csrf-token", factors: ["password"], trustedDevice: false, remoteSession: false, enrollmentRequired: false };
export const INFO = { hostname: "nas", version: "0.0.0-dev", demoMode: false, uptimeSeconds: 90000, capabilities: {}, versions: {}, os: {} };
