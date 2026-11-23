import en from "./en";
import itDict from "./it";
import { getLang, setLang, t } from "./index";

describe("i18n", () => {
  it("Italian has exactly the English keys and no empty strings", () => {
    expect(Object.keys(itDict).sort()).toEqual(Object.keys(en).sort());
    for (const [k, v] of Object.entries(itDict)) expect(v, k).not.toBe("");
  });

  it("keeps placeholders in translations", () => {
    for (const k of Object.keys(en) as (keyof typeof en)[]) {
      const ph = (s: string) => (s.match(/\{\w+\}/g) ?? []).sort().join();
      expect(ph(itDict[k]), k).toBe(ph(en[k]));
    }
  });

  it("switches language and interpolates", () => {
    const before = getLang();
    setLang("it");
    expect(t("nav.security")).toBe("Sicurezza");
    expect(t("shell.serverError", { status: 500 })).toContain("500");
    setLang("en");
    expect(t("nav.security")).toBe("Security");
    setLang(before);
  });
});
