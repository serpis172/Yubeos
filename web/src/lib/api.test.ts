import { ApiError, api, setCsrfToken } from "./api";

type Outcome = (() => Response) | { throws: unknown };

/** Each call gets a fresh Response (bodies can be read only once). */
function mockFetch(outcome: Outcome) {
  const fn = vi.fn(async () => {
    if (typeof outcome === "function") return outcome();
    throw outcome.throws;
  });
  vi.stubGlobal("fetch", fn);
  return fn;
}

const json = (body: unknown, init: ResponseInit = {}) => () =>
  new Response(JSON.stringify(body), { ...init, headers: { "Content-Type": "application/json", ...(init.headers as Record<string, string> | undefined) } });

afterEach(() => vi.unstubAllGlobals());

describe("api()", () => {
  it("returns parsed JSON", async () => {
    mockFetch(json({ ok: true }));
    expect(await api("/x")).toEqual({ ok: true });
  });

  it("handles 204 No Content without parsing a body", async () => {
    mockFetch(() => new Response(null, { status: 204 }));
    expect(await api("/auth/devices/1", { method: "DELETE" })).toBeUndefined();
  });

  it("turns problem+json into ApiError with status", async () => {
    mockFetch(json({ type: "t", title: "Nope", status: 403, detail: "d" }, { status: 403, headers: { "Content-Type": "application/problem+json" } }));
    await expect(api("/x")).rejects.toMatchObject({ name: "ApiError", status: 403, message: "d" });
  });

  it("synthesizes a problem for non-JSON errors (e.g. proxy 502)", async () => {
    mockFetch(() => new Response("<html>bad gateway</html>", { status: 502, statusText: "Bad Gateway" }));
    const e = (await api("/x").catch((x: unknown) => x)) as ApiError;
    expect(e).toBeInstanceOf(ApiError);
    expect(e.status).toBe(502);
  });

  it("reports network failures as status 0", async () => {
    mockFetch({ throws: new TypeError("Failed to fetch") });
    await expect(api("/x")).rejects.toMatchObject({ status: 0, message: "The server could not be reached" });
  });

  it("sends the CSRF header only on state-changing requests", async () => {
    setCsrfToken("tok");
    const f = mockFetch(json({}));
    await api("/x");
    await api("/x", { method: "POST" });
    const [, getInit] = f.mock.calls[0] as unknown as [string, RequestInit];
    const [, postInit] = f.mock.calls[1] as unknown as [string, RequestInit];
    expect(new Headers(getInit.headers).has("X-Yubeos-CSRF")).toBe(false);
    expect(new Headers(postInit.headers).get("X-Yubeos-CSRF")).toBe("tok");
    setCsrfToken("");
  });

  it("lets caller cancellation propagate untouched", async () => {
    const ac = new AbortController();
    ac.abort();
    mockFetch({ throws: new DOMException("aborted", "AbortError") });
    await expect(api("/x", { signal: ac.signal })).rejects.toMatchObject({ name: "AbortError" });
  });
});
