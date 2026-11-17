import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "../app/App";
import { INFO, jsonRes, ME, problem, routeFetch } from "../test/fetchMock";

afterEach(() => { vi.unstubAllGlobals(); window.history.pushState({}, "", "/"); });

const unauth = () => problem(401, "unauthenticated");

describe("login", () => {
  it("shows the sign-in form when there is no session", async () => {
    routeFetch({ "GET /auth/me": unauth() });
    render(<App />);
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Main" })).not.toBeInTheDocument();
  });

  it("maps credential and lockout problems to clear messages and clears the password field", async () => {
    let n = 0;
    routeFetch({
      "GET /auth/me": unauth(),
      "POST /auth/login": () => (++n === 1 ? problem(401, "invalid-credentials") : problem(429, "locked-out")),
    });
    render(<App />);
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Username"), "bob");
    await user.type(screen.getByLabelText("Password"), "wrong-password");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByText("Incorrect username or password.")).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toHaveValue("");
    await user.type(screen.getByLabelText("Password"), "again-wrong");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByText(/Too many failed attempts/)).toBeInTheDocument();
  });

  it("signs in, stores the CSRF token and shows the app", async () => {
    let signedIn = false;
    const { calls } = routeFetch({
      "GET /auth/me": () => (signedIn ? jsonRes(ME) : unauth()),
      "POST /auth/login": (init) => { signedIn = true; expect(JSON.parse(String(init?.body))).toEqual({ username: "bob", password: "correct-horse" }); return jsonRes(ME); },
      "GET /system/info": jsonRes(INFO),
    });
    render(<App />);
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Username"), " bob ");
    await user.type(screen.getByLabelText("Password"), "correct-horse");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("navigation", { name: "Main" })).toBeInTheDocument();
    expect(calls.some((c) => c.key === "POST /auth/login")).toBe(true);
  });

  it("offers Retry (not the login form) when the server is down", async () => {
    routeFetch({ "GET /auth/me": () => { throw new TypeError("Failed to fetch"); } });
    render(<App />);
    expect(await screen.findByText(/Cannot reach the server/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Username")).not.toBeInTheDocument();
  });
});

describe("setup page", () => {
  it("explains what is needed when opened without a token", async () => {
    window.history.pushState({}, "", "/setup");
    routeFetch({});
    render(<App />);
    expect(await screen.findByText(/needs a setup link/i)).toBeInTheDocument();
  });

  it("validates locally, sends the token, then removes it from the address bar", async () => {
    window.history.pushState({}, "", "/setup?token=TOKEN123");
    const { calls } = routeFetch({
      "POST /auth/setup": (init) => { expect(JSON.parse(String(init?.body))).toEqual({ token: "TOKEN123", newPassword: "a-long-enough-password" }); return new Response(null, { status: 204 }); },
    });
    render(<App />);
    await waitFor(() => expect(window.location.search).toBe(""));
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("New password"), "short");
    await user.type(screen.getByLabelText("Repeat password"), "short");
    await user.click(screen.getByRole("button", { name: "Save password" }));
    expect(await screen.findByText(/Use at least 12 characters/)).toBeInTheDocument();
    expect(calls).toHaveLength(0);

    fireEvent.change(screen.getByLabelText("New password"), { target: { value: "a-long-enough-password" } });
    fireEvent.change(screen.getByLabelText("Repeat password"), { target: { value: "different-password-1" } });
    await user.click(screen.getByRole("button", { name: "Save password" }));
    expect(await screen.findByText(/do not match/)).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Repeat password"), { target: { value: "a-long-enough-password" } });
    await user.click(screen.getByRole("button", { name: "Save password" }));
    expect(await screen.findByText(/Password saved/)).toBeInTheDocument();
  });

  it("shows the server's policy message for a rejected password and the expired-link message", async () => {
    window.history.pushState({}, "", "/setup?token=T");
    let first = true;
    routeFetch({
      "POST /auth/setup": () => {
        if (first) { first = false; return problem(422, "validation", { errors: [{ pointer: "/newPassword", message: "password is too common" }] }); }
        return problem(400, "invalid-setup-link");
      },
    });
    render(<App />);
    const user = userEvent.setup();
    const pw = "password12345";
    await user.type(await screen.findByLabelText("New password"), pw);
    await user.type(screen.getByLabelText("Repeat password"), pw);
    await user.click(screen.getByRole("button", { name: "Save password" }));
    expect(await screen.findByText("password is too common")).toBeInTheDocument();
    await user.type(screen.getByLabelText("New password"), pw);
    await user.type(screen.getByLabelText("Repeat password"), pw);
    await user.click(screen.getByRole("button", { name: "Save password" }));
    expect(await screen.findByText(/invalid or has expired/)).toBeInTheDocument();
  });
});
