import { useState } from "react";
import { useT } from "../../i18n";
import styles from "../Auth.module.css";

/** Shows recovery codes once and makes the user acknowledge having stored them. */
export function RecoveryCodes({ codes, onDone, doneLabel }: { codes: string[]; onDone: () => void; doneLabel?: string }) {
  const t = useT();
  const [saved, setSaved] = useState(false);
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(codes.join("\n"));
      setCopied(true);
    } catch { /* clipboard unavailable: the codes stay visible */ }
  }

  return (
    <div>
      <h2>{t("enroll.recoveryTitle")}</h2>
      <p className={styles.hint}>{t("enroll.recoveryHint")}</p>
      <ul className={styles.codes} aria-label={t("enroll.recoveryTitle")}>{codes.map((c) => <li key={c}>{c}</li>)}</ul>
      <button type="button" className={styles.secondary} onClick={() => void copy()}>{copied ? t("enroll.copied") : t("enroll.copy")}</button>
      <label className={styles.check}>
        <input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} />
        <span>{t("enroll.saved")}</span>
      </label>
      <button type="button" className={styles.button} style={{ marginTop: 14 }} disabled={!saved} onClick={onDone}>{doneLabel ?? t("enroll.finish")}</button>
    </div>
  );
}
