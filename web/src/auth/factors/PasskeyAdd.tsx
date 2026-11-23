import { useId, useState, type FormEvent } from "react";
import { passkeyRegisterBegin, passkeyRegisterFinish } from "../../lib/api";
import { useT } from "../../i18n";
import { authErrorMessage } from "../messages";
import { createPasskey, passkeysSupported } from "../webauthn";
import styles from "../Auth.module.css";

/** Registers a passkey: asks for a name, runs the browser ceremony and stores the credential. */
export function PasskeyAdd({ onAdded, onCancel }: { onAdded: () => void; onCancel: () => void }) {
  const t = useT();
  const uid = useId();
  const [label, setLabel] = useState(t("enroll.passkeyNameDefault"));
  const [error, setError] = useState(passkeysSupported() ? "" : t("auth.passkeyUnsupported"));
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const ch = await passkeyRegisterBegin();
      const credential = await createPasskey(ch.webauthn as Record<string, unknown>);
      await passkeyRegisterFinish({ challengeId: ch.challengeId, label: label.trim() || t("enroll.passkeyNameDefault"), credential });
      onAdded();
    } catch (err) {
      setError(authErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={(e) => void submit(e)} noValidate>
      <h2>{t("enroll.passkey")}</h2>
      <div role="alert" aria-live="assertive">{error && <p className={styles.error}>{error}</p>}</div>
      <div className={styles.field}>
        <label htmlFor={`${uid}-l`}>{t("enroll.passkeyName")}</label>
        <input id={`${uid}-l`} maxLength={60} value={label} onChange={(e) => setLabel(e.target.value)} />
      </div>
      <div className={styles.row}>
        <button type="button" className={styles.secondary} onClick={onCancel}>{t("enroll.cancel")}</button>
        <button type="submit" className={styles.button} disabled={busy || !passkeysSupported()}>
          {busy ? t("enroll.passkeyAdding") : t("enroll.passkeyAdd")}
        </button>
      </div>
    </form>
  );
}
