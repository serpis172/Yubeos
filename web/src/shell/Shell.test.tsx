import { fireEvent, render, screen } from "@testing-library/react";
import { App } from "../app/App";
import { INFO, jsonRes, ME, problem, routeFetch } from "../test/fetchMock";
import { resolveTheme } from "./Shell";

afterEach(() => { vi.unstubAllGlobals(); window.history.pushState({}, "", "/"); });

describe("Shell states", () => {
  it("distinguishes an unreachable server from being signed out and offers Retry", async () => {
    routeFetch({
      "GET /auth/me": jsonRes(ME),
      "GET /system/info": () => { throw new TypeError("Failed to fetch"); },
    });
    render(<App />);
    expect(await screen.findByText(/server unreachable/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("mobile menu button toggles the navigation drawer", async () => {
    routeFetch({ "GET /auth/me": jsonRes(ME), "GET /system/info": jsonRes(INFO) });
    render(<App />);
    const btn = await screen.findByRole("button", { name: "Open menu" });
    expect(btn).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(btn);
    expect(screen.getByRole("button", { name: "Close menu" })).toHaveAttribute("aria-expanded", "true");
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.getByRole("button", { name: "Open menu" })).toHaveAttribute("aria-expanded", "false");
  });

  it("has a skip link", async () => {
    routeFetch({ "GET /auth/me": jsonRes(ME), "GET /system/info": problem(401, "unauthenticated") });
    render(<App />);
    expect(await screen.findByRole("link", { name: /skip to content/i })).toHaveAttribute("href", "#main");
  });

  it("signing out calls the API with the CSRF token", async () => {
    const { calls } = routeFetch({
      "GET /auth/me": jsonRes(ME), "GET /system/info": jsonRes(INFO),
      "POST /auth/logout": new Response(null, { status: 204 }),
    });
    const assign = vi.fn();
    vi.stubGlobal("location", { ...window.location, assign });
    render(<App />);
    fireEvent.click(await screen.findByRole("button", { name: "Sign out" }));
    await vi.waitFor(() => expect(assign).toHaveBeenCalledWith("/"));
    const out = calls.find((c) => c.key === "POST /auth/logout");
    expect(new Headers(out?.init?.headers).get("X-Yubeos-CSRF")).toBe("csrf-token");
  });
});

describe("resolveTheme", () => {
  it("maps system to light/dark presets and passes explicit choices through", () => {
    expect(resolveTheme("system", true)).toBe("yubeos-light");
    expect(resolveTheme("system", false)).toBe("yubeos-blue");
    expect(resolveTheme("yubeos-midnight", true)).toBe("yubeos-midnight");
  });
});
