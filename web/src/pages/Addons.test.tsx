import { render, screen } from "@testing-library/react";
import { App } from "../app/App";
import { INFO, jsonRes, ME, routeFetch } from "../test/fetchMock";

afterEach(() => { vi.unstubAllGlobals(); window.history.pushState({}, "", "/"); });

const ITEMS = [
  { id: "jellyfin", name: "Jellyfin", version: "10.10.3-1", state: "running", embed: "link", sidebar: true, url: "http://192.168.1.10:20001/" },
  { id: "kuma", name: "Uptime Kuma", state: "stopped", embed: "link", sidebar: true, urlBlocked: "same-host" },
];

describe("Add-ons page", () => {
  it("lists add-ons and opens them in a new tab without opener access", async () => {
    window.history.pushState({}, "", "/addons");
    routeFetch({ "GET /auth/me": jsonRes(ME), "GET /system/info": jsonRes(INFO), "GET /addons": jsonRes({ items: ITEMS }) });
    render(<App />);
    const link = await screen.findByRole("link", { name: "Open" });
    expect(link).toHaveAttribute("href", "http://192.168.1.10:20001/");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link.getAttribute("rel")).toContain("noopener");
    expect(await screen.findByText("No safe link")).toBeInTheDocument(); // blocked one has no link
    expect(screen.getAllByRole("link", { name: /Jellyfin/ }).length).toBeGreaterThan(0); // sidebar entry too
  });

  it("shows an empty state and a warning when the runtime is down", async () => {
    window.history.pushState({}, "", "/addons");
    routeFetch({ "GET /auth/me": jsonRes(ME), "GET /system/info": jsonRes(INFO), "GET /addons": jsonRes({ items: [], unavailable: true }) });
    render(<App />);
    expect(await screen.findByText("No add-ons installed yet.")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("not reachable");
  });
});
