import { useEffect, useId, useState, type FormEvent } from "react";
import QRCode from "qrcode";
import { totpConfirm, totpEnroll } from "../../lib/api";
import { useT } from "../../i18n";
import { authErrorMessage } from "../messages";
import styles from "../Auth.module.css";

/** Creates a TOTP secret, shows it as QR + text and confirms it with a first code. Resolves with the recovery codes. */
export function TotpEnroll({ onEnrolled, onCancel }: { onEnrolled: (recoveryCodes: string[]) => void; onCancel: () => void }) {
  const t = useT();
  const uid = useId();
  const [secret, setSecret] = useState("");
  const [qr, setQr] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let live = true;
    totpEnroll()
      .then(async (r) => {
        const img = await QRCode.toDataURL(r.otpauthUri, { margin: 2, width: 200, errorCorrectionLevel: "M" });
        if (live) { setSecret(r.secret); setQr(img); }
      })
      .catch((e) => live && setError(authErrorMessage(e)));
    return () => { live = false; };
  }, []);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const r = await totpConfirm(code.trim());
      onEnrolled(r.codes);
    } catch (err) {
      setError(authErrorMessage(err));
      setCode("");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={(e) => void submit(e)} noValidate>
      <h2>{t("enroll.totp")}</h2>
      <p className={styles.hint}>{t("enroll.scan")}</p>
      <div role="alert" aria-live="assertive">{error && <p className={styles.error}>{error}</p>}</div>
      {qr && <img className={styles.qr} src={qr} alt="" width={200} height={200} />}
      {secret && (
        <details>
          <summary>{t("enroll.manualKey")}</summary>
          <p className={styles.secret}>{secret}</p>
        </details>
      )}
      <div className={styles.field} style={{ marginTop: 14 }}>
        <label htmlFor={`${uid}-c`}>{t("auth.code")}</label>
        <input id={`${uid}-c`} inputMode="numeric" autoComplete="one-time-code" maxLength={7} placeholder={t("enroll.codePlaceholder")}
               value={code} onChange={(e) => setCode(e.target.value)} />
      </div>
      <div className={styles.row}>
        <button type="button" className={styles.secondary} onClick={onCancel}>{t("enroll.cancel")}</button>
        <button type="submit" className={styles.button} disabled={busy || code.replace(/\s/g, "").length !== 6}>
          {busy ? t("enroll.confirming") : t("enroll.confirm")}
        </button>
      </div>
    </form>
  );
}
