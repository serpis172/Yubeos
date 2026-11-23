import { createContext, useCallback, useContext, useEffect, useId, useMemo, useRef, useState, type FormEvent, type ReactNode } from "react";
import { stepUp, stepUpBegin, type WebAuthnChallenge } from "../lib/api";
import { useT } from "../i18n";
import { authErrorMessage, problemCode } from "./messages";
import { getPasskey, passkeysSupported } from "./webauthn";
import styles from "./StepUp.module.css";

type Run = <T>(action: () => Promise<T>) => Promise<T>;
const Ctx = createContext<{ run: Run }>({ run: (a) => a() });

/** Wrap a sensitive call: if the server answers "step-up-required", ask for a fresh second factor and retry once. */
export const useStepUp = () => useContext(Ctx).run;

interface Pending { resolve: () => void; reject: (e: unknown) => void }

export function StepUpProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState<Pending | null>(null);
  const run = useCallback<Run>(async (action) => {
    try {
      return await action();
    } catch (e) {
      if (problemCode(e) !== "step-up-required") throw e;
      await new Promise<void>((resolve, reject) => setPending({ resolve, reject }));
      return action();
    }
  }, []);
  const value = useMemo(() => ({ run }), [run]);
  const close = () => setPending(null);
  return (
    <Ctx.Provider value={value}>
      {children}
      {pending && <StepUpDialog onOk={() => { pending.resolve(); close(); }} onCancel={() => { pending.reject(new Error("cancelled")); close(); }} />}
    </Ctx.Provider>
  );
}

function StepUpDialog({ onOk, onCancel }: { onOk: () => void; onCancel: () => void }) {
  const t = useT();
  const uid = useId();
  const [ch, setCh] = useState<WebAuthnChallenge | null>(null);
  const [mode, setMode] = useState<"totp" | "recovery">("totp");
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  useEffect(() => { inputRef.current?.focus(); }, [ch, mode]);

  useEffect(() => {
    let live = true;
    stepUpBegin().then((c) => live && setCh(c)).catch((e) => live && setError(authErrorMessage(e)));
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onCancel(); };
    document.addEventListener("keydown", onKey);
    return () => { live = false; document.removeEventListener("keydown", onKey); };
  }, [onCancel]);

  const methods = ch?.methods ?? [];
  const password = methods.includes("password");
  const canCode = methods.includes("totp");
  const canRecovery = methods.includes("recovery");
  const canPasskey = methods.includes("webauthn") && passkeysSupported();

  const attempt = async (body: Parameters<typeof stepUp>[0]) => {
    setBusy(true);
    setError("");
    try { await stepUp(body); onOk(); } catch (e) { setError(authErrorMessage(e)); setValue(""); } finally { setBusy(false); }
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!ch) return;
    void attempt(password ? { challengeId: ch.challengeId, method: "password", password: value } : { challengeId: ch.challengeId, method: mode, code: value.trim() });
  };

  const withPasskey = async () => {
    if (!ch) return;
    setBusy(true);
    setError("");
    try {
      const credential = await getPasskey(ch.webauthn as Record<string, unknown>);
      await stepUp({ challengeId: ch.challengeId, method: "webauthn", credential });
      onOk();
    } catch (e) { setError(authErrorMessage(e)); } finally { setBusy(false); }
  };

  return (
    <div className={styles.overlay} role="presentation">
      <form className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={`${uid}-t`} onSubmit={submit} noValidate>
        <h2 id={`${uid}-t`}>{t("step.title")}</h2>
        <p className={styles.hint}>{password ? t("step.passwordHint") : t("step.hint")}</p>
        <div role="alert" aria-live="assertive">{error && <p className={styles.error}>{error}</p>}</div>
        {(password || canCode || canRecovery) && (
          <div className={styles.field}>
            <label htmlFor={`${uid}-v`}>{password ? t("auth.password") : mode === "totp" ? t("auth.code") : t("auth.recoveryCode")}</label>
            <input id={`${uid}-v`} type={password ? "password" : "text"} ref={inputRef} autoComplete={password ? "current-password" : "one-time-code"}
                   inputMode={!password && mode === "totp" ? "numeric" : "text"} value={value} onChange={(e) => setValue(e.target.value)} />
          </div>
        )}
        {canPasskey && <button type="button" className={styles.secondary} disabled={busy} onClick={() => void withPasskey()}>{t("auth.usePasskey")}</button>}
        {canCode && canRecovery && (
          <button type="button" className={styles.textButton} onClick={() => { setMode(mode === "totp" ? "recovery" : "totp"); setValue(""); }}>
            {mode === "totp" ? t("auth.useRecovery") : t("auth.useCode")}
          </button>
        )}
        <div className={styles.row}>
          <button type="button" className={styles.secondary} onClick={onCancel}>{t("step.cancel")}</button>
          {(password || canCode || canRecovery) && <button type="submit" className={styles.primary} disabled={busy || !value || !ch}>{t("step.confirm")}</button>}
        </div>
      </form>
    </div>
  );
}
