import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App } from "../app/App";
import { INFO, jsonRes, ME, problem, routeFetch } from "../test/fetchMock";

vi.mock("qrcode", () => ({ default: { toDataURL: vi.fn(async () => "data:image/png;base64,AAAA") } }));

afterEach(() => { vi.unstubAllGlobals(); window.history.pushState({}, "", "/"); });

const FACTORS = { totp: true, passkeys: [], recoveryCodesLeft: 10, required: true };

describe("Security page", () => {
  it("retries a sensitive action after a successful step-up", async () => {
    window.history.pushState({}, "", "/settings/security");
    let fresh = false;
    const { calls } = routeFetch({
      "GET /auth/me": jsonRes(ME),
      "GET /system/info": jsonRes(INFO),
      "GET /auth/factors": jsonRes(FACTORS),
      "GET /auth/devices": jsonRes({ items: [] }),
      "DELETE /auth/totp": () => (fresh ? new Response(null, { status: 204 }) : problem(403, "step-up-required")),
      "POST /auth/stepup/begin": jsonRes({ challengeId: "s1", methods: ["totp"], webauthn: {} }),
      "POST /auth/stepup": () => { fresh = true; return new Response(null, { status: 204 }); },
    });
    render(<App />);
    fireEvent.click(await screen.findByRole("button", { name: "Remove" }));
    await screen.findByRole("dialog");
    fireEvent.change(await screen.findByLabelText("Authenticator code"), { target: { value: "111222" } });
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await waitFor(() => expect(calls.filter((c) => c.key === "DELETE /auth/totp")).toHaveLength(2));
    expect(calls.some((c) => c.key === "POST /auth/stepup")).toBe(true);
  });

  it("hides the remote-access card from non-admins and shows the checklist to admins", async () => {
    window.history.pushState({}, "", "/settings/security");
    routeFetch({
      "GET /auth/me": jsonRes({ ...ME, role: "admin" }),
      "GET /system/info": jsonRes(INFO),
      "GET /auth/factors": jsonRes(FACTORS),
      "GET /auth/devices": jsonRes({ items: [] }),
      "GET /security/remote-status": jsonRes({ exposure: "lan", lockdown: false, checks: [{ id: "tls", status: "warn", title: "Certificate trusted by browsers" }] }),
    });
    render(<App />);
    expect(await screen.findByText("Certificate trusted by browsers")).toBeInTheDocument();
    expect(screen.getByText("WARN")).toBeInTheDocument();
  });

  it("does not request the remote status for regular users", async () => {
    window.history.pushState({}, "", "/settings/security");
    const { calls } = routeFetch({
      "GET /auth/me": jsonRes(ME), "GET /system/info": jsonRes(INFO),
      "GET /auth/factors": jsonRes(FACTORS), "GET /auth/devices": jsonRes({ items: [] }),
    });
    render(<App />);
    await screen.findByText("Second factors");
    expect(calls.some((c) => c.key === "GET /security/remote-status")).toBe(false);
  });
});

describe("Appearance page", () => {
  it("saves a customised theme and rejects an invalid import", async () => {
    window.history.pushState({}, "", "/settings/appearance");
    routeFetch({ "GET /auth/me": jsonRes(ME), "GET /system/info": jsonRes(INFO) });
    render(<App />);
    fireEvent.click(await screen.findByRole("button", { name: "Customize this theme" }));
    expect(await screen.findByTestId("theme-preview")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Save theme" }));
    await waitFor(() => expect(localStorage.getItem("yubeos.themes")).toContain("custom"));

    fireEvent.change(screen.getByLabelText("JSON"), { target: { value: "{\"schemaVersion\":1}" } });
    fireEvent.click(screen.getByRole("button", { name: "Import" }));
    expect(await screen.findByText(/not valid/i)).toBeInTheDocument();
  });
});
