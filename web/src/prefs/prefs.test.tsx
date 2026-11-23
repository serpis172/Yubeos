import { act, render, screen, waitFor } from "@testing-library/react";
import { PrefsProvider, usePrefs } from "./PrefsProvider";
import { jsonRes, routeFetch } from "../test/fetchMock";
import { getThemeState, setChoice } from "../theme/engine";
import { getLang, setLang } from "../i18n";

function Probe() {
  const { units, temperature, setUnits } = usePrefs();
  return <div><span data-testid="u">{units}/{temperature}</span><button onClick={() => setUnits("bits")}>bits</button></div>;
}

const SERVER = { theme: "yubeos-light", language: "it", units: "bits", temperature: "f", customThemes: [], updatedAt: 1234 };

describe("PrefsProvider", () => {
  beforeEach(() => { localStorage.clear(); setLang("en"); setChoice("system"); });
  afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers(); });

  it("applies the server's preferences when they exist (settings follow the user)", async () => {
    const { calls } = routeFetch({ "GET /me/preferences": jsonRes(SERVER) });
    render(<PrefsProvider><Probe /></PrefsProvider>);
    await waitFor(() => expect(screen.getByTestId("u")).toHaveTextContent("bits/f"));
    expect(getThemeState().choice).toBe("yubeos-light");
    expect(getLang()).toBe("it");
    expect(localStorage.getItem("yubeos.theme")).toBe("yubeos-light"); // cached for the pre-paint script
    expect(calls.some((c) => c.key === "PUT /me/preferences")).toBe(false); // applying server state must not echo back
  });

  it("uploads the browser's current settings when the server has none (first sign-in migration)", async () => {
    setChoice("yubeos-midnight");
    const { calls } = routeFetch({
      "GET /me/preferences": jsonRes({ theme: "system", language: "en", units: "bytes", temperature: "c", customThemes: [], updatedAt: 0 }),
      "PUT /me/preferences": () => jsonRes({ ...SERVER }),
    });
    render(<PrefsProvider><Probe /></PrefsProvider>);
    await waitFor(() => expect(calls.some((c) => c.key === "PUT /me/preferences")).toBe(true), { timeout: 3000 });
    const sent = JSON.parse(String(calls.find((c) => c.key === "PUT /me/preferences")!.init!.body));
    expect(sent.theme).toBe("yubeos-midnight");
  });

  it("uploads a later change after a short debounce", async () => {
    const { calls } = routeFetch({ "GET /me/preferences": jsonRes(SERVER), "PUT /me/preferences": () => jsonRes(SERVER) });
    render(<PrefsProvider><Probe /></PrefsProvider>);
    await waitFor(() => expect(screen.getByTestId("u")).toHaveTextContent("bits/f"));
    act(() => { setLang("en"); });
    await waitFor(() => expect(calls.some((c) => c.key === "PUT /me/preferences")).toBe(true), { timeout: 3000 });
    expect(JSON.parse(String(calls.find((c) => c.key === "PUT /me/preferences")!.init!.body)).language).toBe("en");
  });
});
