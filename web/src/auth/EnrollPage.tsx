import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useT } from "../i18n";
import { passkeysSupported } from "./webauthn";
import { PasskeyAdd } from "./factors/PasskeyAdd";
import { RecoveryCodes } from "./factors/RecoveryCodes";
import { TotpEnroll } from "./factors/TotpEnroll";
import { ME_KEY } from "./useMe";
import styles from "./Auth.module.css";

type Step = { kind: "choose" } | { kind: "passkey" } | { kind: "totp" } | { kind: "codes"; codes: string[] };

/** Enrolment wizard: shown instead of the app while the session is enrolment-only (policy demands a second factor). */
export function EnrollPage() {
  const t = useT();
  const qc = useQueryClient();
  const [step, setStep] = useState<Step>({ kind: "choose" });
  const finish = () => void qc.invalidateQueries({ queryKey: ME_KEY }); // the server cleared the flag: /auth/me now unlocks the app

  return (
    <main className={styles.page}>
      <div className={`${styles.card} ${styles.wide}`}>
        <div className={styles.brand}><span className={styles.logo} aria-hidden="true">Y</span>Yubeos</div>
        {step.kind === "choose" && (
          <>
            <h1>{t("enroll.title")}</h1>
            <p className={styles.hint}>{t("enroll.intro")}</p>
            <div className={styles.choices}>
              <button type="button" className={styles.choice} disabled={!passkeysSupported()} onClick={() => setStep({ kind: "passkey" })}>
                <b>{t("enroll.passkey")}{passkeysSupported() && <em className={styles.badge}>{t("enroll.recommended")}</em>}</b>
                <span>{passkeysSupported() ? t("enroll.passkeyDesc") : t("auth.passkeyUnsupported")}</span>
              </button>
              <button type="button" className={styles.choice} onClick={() => setStep({ kind: "totp" })}>
                <b>{t("enroll.totp")}</b>
                <span>{t("enroll.totpDesc")}</span>
              </button>
            </div>
          </>
        )}
        {step.kind === "passkey" && <PasskeyAdd onAdded={finish} onCancel={() => setStep({ kind: "choose" })} />}
        {step.kind === "totp" && <TotpEnroll onEnrolled={(codes) => setStep({ kind: "codes", codes })} onCancel={() => setStep({ kind: "choose" })} />}
        {step.kind === "codes" && <RecoveryCodes codes={step.codes} onDone={finish} />}
      </div>
    </main>
  );
}
