import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App } from "../app/App";
import { INFO, jsonRes, ME, problem, routeFetch } from "../test/fetchMock";

vi.mock("qrcode", () => ({ default: { toDataURL: vi.fn(async () => "data:image/png;base64,AAAA") } }));

afterEach(() => { vi.unstubAllGlobals(); window.history.pushState({}, "", "/"); });

const signedOut = problem(401, "unauthenticated");

describe("second-factor login", () => {
  it("asks for a code after the password, sends trustDevice and signs in", async () => {
    let me = false;
    const { calls } = routeFetch({
      "GET /auth/me": () => (me ? jsonRes(ME) : signedOut.clone()),
      "GET /system/info": jsonRes(INFO),
      "POST /auth/login": jsonRes({ needs2fa: true, methods: ["totp", "recovery"], challengeId: "c1" }, 202),
      "POST /auth/2fa/verify": () => { me = true; return jsonRes(ME); },
    });
    render(<App />);
    fireEvent.change(await screen.findByLabelText("Username"), { target: { value: "bob" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "correct horse battery" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("heading", { name: "Confirm it is you" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Authenticator code"), { target: { value: "123456" } });
    fireEvent.click(screen.getByLabelText(/Trust this device/));
    fireEvent.click(screen.getByRole("button", { name: "Verify" }));

    await waitFor(() => expect(calls.some((c) => c.key === "POST /auth/2fa/verify")).toBe(true));
    const sent = JSON.parse(String(calls.find((c) => c.key === "POST /auth/2fa/verify")!.init!.body));
    expect(sent).toEqual({ challengeId: "c1", method: "totp", code: "123456", trustDevice: true });
    expect(await screen.findByText("bob")).toBeInTheDocument();
  });

  it("switches to a recovery code and shows a rejected code", async () => {
    routeFetch({
      "GET /auth/me": signedOut,
      "POST /auth/login": jsonRes({ needs2fa: true, methods: ["totp", "recovery"], challengeId: "c1" }, 202),
      "POST /auth/2fa/verify": problem(401, "invalid-second-factor"),
    });
    render(<App />);
    fireEvent.change(await screen.findByLabelText("Username"), { target: { value: "bob" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "correct horse battery" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    fireEvent.click(await screen.findByRole("button", { name: "Use a recovery code" }));
    fireEvent.change(screen.getByLabelText("Recovery code"), { target: { value: "ABCDE-FGHIJ" } });
    fireEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(await screen.findByText(/not accepted/i)).toBeInTheDocument();
  });
});

describe("enrolment wizard", () => {
  it("replaces the app while enrolment is required and walks TOTP → recovery codes", async () => {
    let enrolled = false;
    const { calls } = routeFetch({
      "GET /auth/me": () => jsonRes({ ...ME, enrollmentRequired: !enrolled }),
      "GET /system/info": jsonRes(INFO),
      "POST /auth/totp/enroll": jsonRes({ secret: "JBSWY3DPEHPK3PXP", otpauthUri: "otpauth://totp/Yubeos:bob?secret=JBSWY3DPEHPK3PXP" }),
      "POST /auth/totp/confirm": () => { enrolled = true; return jsonRes({ codes: ["AAAAA-11111", "BBBBB-22222"] }); },
    });
    render(<App />);
    fireEvent.click(await screen.findByRole("button", { name: /Authenticator app/ }));
    expect(await screen.findByText("JBSWY3DPEHPK3PXP")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Authenticator code"), { target: { value: "654321" } });
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    expect(await screen.findByText("AAAAA-11111")).toBeInTheDocument();
    const finish = screen.getByRole("button", { name: "Finish" });
    expect(finish).toBeDisabled(); // must acknowledge storing the codes first
    fireEvent.click(screen.getByLabelText(/stored these codes/));
    fireEvent.click(finish);
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument();
    expect(calls.some((c) => c.key === "POST /auth/totp/confirm")).toBe(true);
  });
});
