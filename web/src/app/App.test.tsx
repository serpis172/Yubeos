import { render, screen } from "@testing-library/react";
import { App } from "./App";

describe("App shell", () => {
  it("renders the sidebar and the placeholder for the current route", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(
      JSON.stringify({ type: "x", title: "Authentication required", status: 401 }),
      { status: 401, headers: { "Content-Type": "application/problem+json" } },
    )));
    render(<App />);
    expect(screen.getByRole("navigation", { name: "Main" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Disk Manager/ })).toHaveAttribute("href", "/disks");
    expect(await screen.findByText(/not signed in/i)).toBeInTheDocument();
  });
});
