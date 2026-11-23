import { useEffect, useId, useState, type FormEvent, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { PasskeyAdd } from "../auth/factors/PasskeyAdd";
import { RecoveryCodes } from "../auth/factors/RecoveryCodes";
import { TotpEnroll } from "../auth/factors/TotpEnroll";
import { authErrorMessage } from "../auth/messages";
import { StepUpProvider, useStepUp } from "../auth/StepUp";
import { useMe } from "../auth/useMe";
import {
  changePassword, getFactors, getRemoteStatus, listDevices, passkeyRemove, regenerateRecoveryCodes, revokeAllDevices, revokeDevice, setLockdown, totpRemove,
} from "../lib/api";
import { getLang, useT } from "../i18n";
import styles from "./Page.module.css";

const fmt = (iso: string | undefined) => (iso ? new Date(iso).toLocaleDateString(getLang(), { year: "numeric", month: "short", day: "numeric" }) : "");

function Card({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <section className={styles.card} aria-label={title}>
      <h2>{title}</h2>
      {hint && <p className={styles.hint}>{hint}</p>}
      {children}
    </section>
  );
}

export function SecurityPage() {
  const t = useT();
  useEffect(() => { document.title = `${t("sec.title")} · Yubeos`; }, [t]);
  const me = useMe();
  return (
    <StepUpProvider>
      <div className={styles.page}>
        <h1>{t("sec.title")}</h1>
        <FactorsCard />
        <DevicesCard />
        <PasswordCard />
        {me.data?.role === "admin" && <RemoteCard />}
      </div>
    </StepUpProvider>
  );
}

type Adding = null | "totp" | "passkey" | { codes: string[] };

function FactorsCard() {
  const t = useT();
  const qc = useQueryClient();
  const stepUp = useStepUp();
  const q = useQuery({ queryKey: ["factors"], queryFn: ({ signal }) => getFactors(signal) });
  const [adding, setAdding] = useState<Adding>(null);
  const [error, setError] = useState("");
  const refresh = () => void qc.invalidateQueries({ queryKey: ["factors"] });
  const act = async (fn: () => Promise<unknown>) => {
    setError("");
    try { await stepUp(fn); refresh(); } catch (e) { if (!(e instanceof Error && e.message === "cancelled")) setError(authErrorMessage(e)); }
  };

  if (q.isPending) return <Card title={t("sec.factors")}><p className={styles.hint}>{t("sec.loading")}</p></Card>;
  if (q.isError) return <Card title={t("sec.factors")}><p className={styles.error}>{t("sec.loadFailed")}</p></Card>;
  const f = q.data;

  if (adding === "totp") return <Card title={t("sec.factors")}><TotpEnroll onEnrolled={(codes) => { refresh(); setAdding({ codes }); }} onCancel={() => setAdding(null)} /></Card>;
  if (adding === "passkey") return <Card title={t("sec.factors")}><PasskeyAdd onAdded={() => { refresh(); setAdding(null); }} onCancel={() => setAdding(null)} /></Card>;
  if (adding && typeof adding === "object") return <Card title={t("sec.factors")}><RecoveryCodes codes={adding.codes} onDone={() => setAdding(null)} /></Card>;

  return (
    <Card title={t("sec.factors")} hint={t("sec.factorsHint") + (f.required ? " " + t("sec.required") : "")}>
      {error && <p className={styles.error} role="alert">{error}</p>}
      <ul className={styles.list}>
        <li className={styles.item}>
          <div className={styles.grow}><b>{t("sec.totp")}</b></div>
          <span className={`${styles.pill} ${f.totp ? styles.ok : ""}`}>{f.totp ? t("sec.totpOn") : t("sec.totpOff")}</span>
          {f.totp
            ? <button type="button" className={styles.danger} onClick={() => void act(totpRemove)}>{t("sec.remove")}</button>
            : <button type="button" className={styles.button} onClick={() => setAdding("totp")}>{t("sec.enable")}</button>}
        </li>
      </ul>
      <h3>{t("sec.passkeys")}</h3>
      {f.passkeys.length === 0 ? <p className={styles.muted}>{t("sec.passkeysNone")}</p> : (
        <ul className={styles.list}>
          {f.passkeys.map((p) => (
            <li key={p.id} className={styles.item}>
              <div className={styles.grow}>
                <b>{p.label}</b>
                <span className={styles.muted}>{t("sec.created", { date: fmt(p.createdAt) })} · {p.lastUsedAt ? t("sec.lastUsed", { date: fmt(p.lastUsedAt) }) : t("sec.never")}</span>
              </div>
              {p.flagged && <span className={`${styles.pill} ${styles.fail}`}>{t("sec.passkeyFlagged")}</span>}
              <button type="button" className={styles.danger} onClick={() => void act(() => passkeyRemove(p.id))}>{t("sec.remove")}</button>
            </li>
          ))}
        </ul>
      )}
      <div className={styles.row}>
        <button type="button" className={styles.secondary} onClick={() => setAdding("passkey")}>{t("sec.passkeyAdd")}</button>
      </div>
      <h3>{t("sec.recovery")}</h3>
      <div className={styles.row}>
        <span className={styles.muted}>{t("sec.recoveryLeft", { n: f.recoveryCodesLeft })} · {t("sec.recoveryRegenHint")}</span>
        <button type="button" className={styles.secondary} disabled={!f.totp}
                onClick={() => void act(async () => { const r = await regenerateRecoveryCodes(); setAdding({ codes: r.codes }); })}>
          {t("sec.recoveryRegen")}
        </button>
      </div>
    </Card>
  );
}

function DevicesCard() {
  const t = useT();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["devices"], queryFn: ({ signal }) => listDevices(signal) });
  const refresh = () => void qc.invalidateQueries({ queryKey: ["devices"] });
  return (
    <Card title={t("sec.devices")} hint={t("sec.devicesHint")}>
      {q.isPending && <p className={styles.hint}>{t("sec.loading")}</p>}
      {q.isError && <p className={styles.error}>{t("sec.loadFailed")}</p>}
      {q.data && (
        <ul className={styles.list}>
          {q.data.items.map((d) => (
            <li key={d.id} className={styles.item}>
              <div className={styles.grow}>
                <b>{d.label}{d.current && <> · <span className={styles.muted}>{t("sec.thisDevice")}</span></>}</b>
                <span className={styles.muted}>
                  {d.kind === "session" ? t("sec.kindSession") : t("sec.kindTrusted")}{d.lastIp ? ` · ${d.lastIp}` : ""} · {fmt(d.lastUsedAt)}{d.expiresAt ? ` · ${t("sec.expires", { date: fmt(d.expiresAt) })}` : ""}
                </span>
              </div>
              {!d.current && <button type="button" className={styles.danger} onClick={() => void revokeDevice(d.id).then(refresh)}>{t("sec.revoke")}</button>}
            </li>
          ))}
        </ul>
      )}
      <div className={styles.row}>
        <button type="button" className={styles.danger} onClick={() => void revokeAllDevices().finally(() => window.location.assign("/"))}>{t("sec.revokeAll")}</button>
      </div>
    </Card>
  );
}

function PasswordCard() {
  const t = useT();
  const uid = useId();
  const [cur, setCur] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (next.length < 12) return setMsg({ ok: false, text: t("sec.passwordShort") });
    if (next !== again) return setMsg({ ok: false, text: t("sec.passwordMismatch") });
    setBusy(true);
    setMsg(null);
    try {
      await changePassword(cur, next);
      setMsg({ ok: true, text: t("sec.passwordChanged") });
      setCur(""); setNext(""); setAgain("");
    } catch (err) {
      setMsg({ ok: false, text: authErrorMessage(err) });
    } finally { setBusy(false); }
  }

  return (
    <Card title={t("sec.password")}>
      <form onSubmit={(e) => void submit(e)} className={styles.panel} noValidate>
        {msg && <p className={msg.ok ? styles.success : styles.error} role="alert">{msg.text}</p>}
        <div className={styles.field}><label htmlFor={`${uid}-c`}>{t("sec.currentPassword")}</label>
          <input id={`${uid}-c`} type="password" autoComplete="current-password" value={cur} onChange={(e) => setCur(e.target.value)} /></div>
        <div className={styles.field}><label htmlFor={`${uid}-n`}>{t("sec.newPassword")}</label>
          <input id={`${uid}-n`} type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} /></div>
        <div className={styles.field}><label htmlFor={`${uid}-a`}>{t("sec.confirmPassword")}</label>
          <input id={`${uid}-a`} type="password" autoComplete="new-password" value={again} onChange={(e) => setAgain(e.target.value)} /></div>
        <div className={styles.row}><button type="submit" className={styles.button} disabled={busy || !cur || !next}>{t("sec.changePassword")}</button></div>
      </form>
    </Card>
  );
}

function RemoteCard() {
  const t = useT();
  const qc = useQueryClient();
  const stepUp = useStepUp();
  const q = useQuery({ queryKey: ["remote-status"], queryFn: ({ signal }) => getRemoteStatus(signal) });
  const [error, setError] = useState("");
  if (q.isPending) return <Card title={t("sec.remote")}><p className={styles.hint}>{t("sec.loading")}</p></Card>;
  if (q.isError) return <Card title={t("sec.remote")}><p className={styles.error}>{t("sec.loadFailed")}</p></Card>;
  const s = q.data;
  const toggle = async () => {
    setError("");
    try {
      await stepUp(() => setLockdown(!s.lockdown));
      void qc.invalidateQueries({ queryKey: ["remote-status"] });
    } catch (e) { if (!(e instanceof Error && e.message === "cancelled")) setError(authErrorMessage(e)); }
  };
  return (
    <Card title={t("sec.remote")} hint={t("sec.remoteHint")}>
      {error && <p className={styles.error} role="alert">{error}</p>}
      <p><b>{t("sec.exposure")}:</b> {s.exposure === "remote" ? t("sec.exposureRemote") : t("sec.exposureLan")}</p>
      <ul className={styles.list}>
        {s.checks.map((c) => (
          <li key={c.id} className={styles.item}>
            <span className={`${styles.pill} ${styles[c.status]}`}>{c.status.toUpperCase()}</span>
            <div className={styles.grow}><b>{c.title}</b>{c.detail && <span className={styles.muted}>{c.detail}</span>}</div>
          </li>
        ))}
      </ul>
      <div className={styles.row}>
        <span><b>{t("sec.lockdown")}:</b> {s.lockdown ? t("sec.lockdownOn") : t("sec.lockdownOff")}</span>
        <button type="button" className={s.lockdown ? styles.secondary : styles.danger} onClick={() => void toggle()}>
          {s.lockdown ? t("sec.lockdownDisable") : t("sec.lockdownEnable")}
        </button>
      </div>
      <p className={styles.muted}>{t("sec.lockdownHint")}</p>
    </Card>
  );
}
