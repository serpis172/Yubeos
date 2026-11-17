import { Outlet } from "react-router-dom";
import { ApiError } from "../lib/api";
import { LoginPage } from "./LoginPage";
import { useMe } from "./useMe";
import styles from "./Auth.module.css";

/** Renders the login page on 401, a retryable error on other failures, and the app once signed in. */
export function RequireAuth() {
  const me = useMe();
  if (me.isPending) return <main className={styles.page} aria-busy="true"><p className={styles.hint}>Loading…</p></main>;
  if (me.isError) {
    if (me.error instanceof ApiError && me.error.status === 401) return <LoginPage />;
    return (
      <main className={styles.page}>
        <div className={styles.card} role="alert">
          <h1>Cannot reach the server</h1>
          <p className={styles.hint}>Check that Yubeos is running and your network connection, then try again.</p>
          <button className={styles.button} type="button" onClick={() => void me.refetch()}>Retry</button>
        </div>
      </main>
    );
  }
  return <Outlet />;
}
