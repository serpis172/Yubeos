import { render, screen } from "@testing-library/react";
import { App } from "./App";
import { INFO, jsonRes, ME, routeFetch } from "../test/fetchMock";

afterEach(() => { vi.unstubAllGlobals(); window.history.pushState({}, "", "/"); });

describe("App shell (signed in)", () => {
  it("renders the sidebar, the user and the host once signed in", async () => {
    routeFetch({ "GET /auth/me": jsonRes(ME), "GET /system/info": jsonRes(INFO) });
    render(<App />);
    expect(await screen.findByRole("navigation", { name: "Main" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Disk Manager/ })).toHaveAttribute("href", "/disks");
    expect(await screen.findByText("nas")).toBeInTheDocument();
    expect(screen.getByText("bob")).toBeInTheDocument();
  });

  it("shows the enrollment banner for enrollment-only sessions", async () => {
    routeFetch({ "GET /auth/me": jsonRes({ ...ME, enrollmentRequired: true }), "GET /system/info": jsonRes(INFO) });
    render(<App />);
    expect(await screen.findByText(/must use a second factor/i)).toBeInTheDocument();
  });
});
