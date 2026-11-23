import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { getPasskey, passkeysSupported } from "./webauthn";
import { login, setCsrfToken, verifySecondFactor, webauthnLoginBegin, webauthnLoginFinish, type Me, type SecondFactorChallenge } from "../lib/api";
import { useT } from "../i18n";
import { authErrorMessage } from "./messages";
import { ME_KEY } from "./useMe";
import styles from "./Auth.module.css";

type Step = { kind: "password" } | { kind: "second"; challenge: SecondFactorChallenge };

export function LoginPage() {
  const t = useT();
  const qc = useQueryClient();
  const [step, setStep] = useState<Step>({ kind: "password" });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => { document.title = `${t("auth.signIn")} · Yubeos`; }, [t]);

  const done = (me: Me) => { setCsrfToken(me.csrf); qc.setQueryData(ME_KEY, me); };
  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try { await fn(); } catch (e) { setError(authErrorMessage(e)); } finally { setBusy(false); }
  };

  return (
    <main className={styles.page}>
      <div className={styles.card}>
        <div className={styles.brand}><span className={styles.logo} aria-hidden="true">Y</span>Yubeos</div>
        <div role="alert" aria-live="assertive">{error && <p className={styles.error}>{error}</p>}</div>
        {step.kind === "password"
          ? <PasswordStep busy={busy} run={run} onDone={done} onSecond={(c) => setStep({ kind: "second", challenge: c })} t={t} />
          : <SecondStep challenge={step.challenge} busy={busy} run={run} onDone={done} onBack={() => { setStep({ kind: "password" }); setError(""); }} t={t} />}
      </div>
    </main>
  );
}

interface StepProps {
  busy: boolean;
  run: (fn: () => Promise<void>) => Promise<void>;
  onDone: (me: Me) => void;
  t: ReturnType<typeof useT>;
}

function PasswordStep({ busy, run, onDone, onSecond, t }: StepProps & { onSecond: (c: SecondFactorChallenge) => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [trust, setTrust] = useState(false);
  const uid = useId();

  const submit = (e: FormEvent) => {
    e.preventDefault();
    void run(async () => {
      try {
        const out = await login(username.trim(), password);
        if (out.challenge) onSecond(out.challenge); else onDone(out.me);
      } finally { setPassword(""); }
    });
  };

  const withPasskey = () => void run(async () => {
    const ch = await webauthnLoginBegin();
    const credential = await getPasskey(ch.webauthn as Record<string, unknown>);
    onDone(await webauthnLoginFinish({ challengeId: ch.challengeId, trustDevice: trust, credential }));
  });

  return (
    <form onSubmit={submit} noValidate>
      <h1>{t("auth.signIn")}</h1>
      <p className={styles.hint}>{t("auth.signInHint")}</p>
      {passkeysSupported() && (
        <>
          <button className={styles.button} type="button" disabled={busy} onClick={withPasskey}>{t("auth.passkeyButton")}</button>
          <p className={styles.divider}><span>{t("auth.or")}</span></p>
        </>
      )}
      <div className={styles.field}>
        <label htmlFor={`${uid}-u`}>{t("auth.username")}</label>
        <input id={`${uid}-u`} name="username" autoComplete="username webauthn" autoCapitalize="none" spellCheck={false}
               required value={username} onChange={(e) => setUsername(e.target.value)} />
      </div>
      <div className={styles.field}>
        <label htmlFor={`${uid}-p`}>{t("auth.password")}</label>
        <input id={`${uid}-p`} name="password" type="password" autoComplete="current-password"
               required value={password} onChange={(e) => setPassword(e.target.value)} />
      </div>
      <button className={styles.button} type="submit" disabled={busy || !username || !password}>
        {busy ? t("auth.signingIn") : t("auth.signIn")}
      </button>
      <label className={styles.check}>
        <input type="checkbox" checked={trust} onChange={(e) => setTrust(e.target.checked)} />
        <span>{t("auth.trustDevice")}</span>
      </label>
      <p className={styles.footnote}>{t("auth.firstTime")}</p>
    </form>
  );
}

function SecondStep({ challenge, busy, run, onDone, onBack, t }: StepProps & { challenge: SecondFactorChallenge; onBack: () => void }) {
  const methods = challenge.methods;
  const canPasskey = methods.includes("webauthn") && passkeysSupported();
  const canCode = methods.includes("totp");
  const canRecovery = methods.includes("recovery");
  const [mode, setMode] = useState<"totp" | "recovery">(canCode ? "totp" : "recovery");
  const [code, setCode] = useState("");
  const [trust, setTrust] = useState(false);
  const uid = useId();
  const codeRef = useRef<HTMLInputElement>(null);
  useEffect(() => { codeRef.current?.focus(); }, [mode]);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    void run(async () => {
      try {
        onDone(await verifySecondFactor({ challengeId: challenge.challengeId ?? "", method: mode, code: code.trim(), trustDevice: trust }));
      } finally { setCode(""); }
    });
  };

  const withPasskey = () => void run(async () => {
    const ch = await webauthnLoginBegin(challenge.challengeId);
    const credential = await getPasskey(ch.webauthn as Record<string, unknown>);
    onDone(await webauthnLoginFinish({ challengeId: ch.challengeId, trustDevice: trust, credential }));
  });

  return (
    <form onSubmit={submit} noValidate>
      <h1>{t("auth.secondTitle")}</h1>
      <p className={styles.hint}>{mode === "totp" ? t("auth.secondHintCode") : t("auth.secondHintRecovery")}</p>
      {(canCode || canRecovery) && (
        <div className={styles.field}>
          <label htmlFor={`${uid}-c`}>{mode === "totp" ? t("auth.code") : t("auth.recoveryCode")}</label>
          <input id={`${uid}-c`} name="code" inputMode={mode === "totp" ? "numeric" : "text"} autoComplete="one-time-code"
                 ref={codeRef} spellCheck={false} required value={code} onChange={(e) => setCode(e.target.value)} />
        </div>
      )}
      {(canCode || canRecovery) && (
        <button className={styles.button} type="submit" disabled={busy || !code}>{busy ? t("auth.verifying") : t("auth.verify")}</button>
      )}
      {canPasskey && (
        <button className={styles.secondary} type="button" disabled={busy} onClick={withPasskey}>{t("auth.usePasskey")}</button>
      )}
      <label className={styles.check}>
        <input type="checkbox" checked={trust} onChange={(e) => setTrust(e.target.checked)} />
        <span>{t("auth.trustDevice")}<small>{t("auth.trustDeviceHint")}</small></span>
      </label>
      <p className={styles.links}>
        {canCode && canRecovery && (
          <button type="button" className={styles.textButton} onClick={() => { setMode(mode === "totp" ? "recovery" : "totp"); setCode(""); }}>
            {mode === "totp" ? t("auth.useRecovery") : t("auth.useCode")}
          </button>
        )}
        <button type="button" className={styles.textButton} onClick={onBack}>{t("auth.back")}</button>
      </p>
    </form>
  );
}
