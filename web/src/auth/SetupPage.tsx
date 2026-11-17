import { useEffect, useId, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { setupPassword } from "../lib/api";
import { authErrorMessage } from "./messages";
import styles from "./Auth.module.css";

const MIN_LENGTH = 12;

/** Consumes a one-time setup link: /setup?token=… . The token is removed from the address bar immediately. */
export function SetupPage() {
  const [token] = useState(() => new URLSearchParams(window.location.search).get("token") ?? "");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  const [busy, setBusy] = useState(false);
  const uid = useId();

  useEffect(() => {
    document.title = "Set your password · Yubeos";
    if (window.location.search) window.history.replaceState(null, "", window.location.pathname);
  }, []);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (password.length < MIN_LENGTH) return setError(`Use at least ${MIN_LENGTH} characters.`);
    if (password !== confirm) return setError("The two passwords do not match.");
    setBusy(true);
    setError("");
    try {
      await setupPassword(token, password);
      setDone(true);
    } catch (err) {
      setError(authErrorMessage(err));
    } finally {
      setBusy(false);
      setPassword("");
      setConfirm("");
    }
  }

  return (
    <main className={styles.page}>
      <form className={styles.card} onSubmit={onSubmit} noValidate>
        <div className={styles.brand}><span className={styles.logo} aria-hidden="true">Y</span>Yubeos</div>
        <h1>Set your password</h1>
        {done ? (
          <>
            <p className={`${styles.error} ${styles.ok}`} role="status">Password saved. You can sign in now.</p>
            <Link className={styles.link} to="/">Go to sign in</Link>
          </>
        ) : !token ? (
          <p className={styles.error} role="alert">This page needs a setup link. Ask the administrator to run <code>yubeosctl user add</code>.</p>
        ) : (
          <>
            <p className={styles.hint}>Choose a password of at least {MIN_LENGTH} characters. A long phrase works best.</p>
            <div role="alert" aria-live="assertive">{error && <p className={styles.error}>{error}</p>}</div>
            <div className={styles.field}>
              <label htmlFor={`${uid}-n`}>New password</label>
              <input id={`${uid}-n`} type="password" autoComplete="new-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
            </div>
            <div className={styles.field}>
              <label htmlFor={`${uid}-c`}>Repeat password</label>
              <input id={`${uid}-c`} type="password" autoComplete="new-password" required value={confirm} onChange={(e) => setConfirm(e.target.value)} />
            </div>
            <button className={styles.button} type="submit" disabled={busy || !password || !confirm}>{busy ? "Saving…" : "Save password"}</button>
          </>
        )}
      </form>
    </main>
  );
}
