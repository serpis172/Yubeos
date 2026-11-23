import { Outlet } from "react-router-dom";
import { ApiError } from "../lib/api";
import { useT } from "../i18n";
import { EnrollPage } from "./EnrollPage";
import { LoginPage } from "./LoginPage";
import { useMe } from "./useMe";
import styles from "./Auth.module.css";

/** Login page on 401, enrolment wizard while a second factor is required but missing, the app once signed in. */
export function RequireAuth() {
  const t = useT();
  const me = useMe();
  if (me.isPending) return <main className={styles.page} aria-busy="true"><p className={styles.hint}>{t("shell.loading")}</p></main>;
  if (me.isError) {
    if (me.error instanceof ApiError && me.error.status === 401) return <LoginPage />;
    return (
      <main className={styles.page}>
        <div className={styles.card} role="alert">
          <h1>{t("shell.cannotReach")}</h1>
          <p className={styles.hint}>{t("shell.cannotReachHint")}</p>
          <button className={styles.button} type="button" onClick={() => void me.refetch()}>{t("shell.retry")}</button>
        </div>
      </main>
    );
  }
  if (me.data.enrollmentRequired) return <EnrollPage />;
  return <Outlet />;
}
