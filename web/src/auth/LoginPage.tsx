import { useEffect, useId, useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { login, setCsrfToken } from "../lib/api";
import { authErrorMessage } from "./messages";
import { ME_KEY } from "./useMe";
import styles from "./Auth.module.css";

export function LoginPage() {
  const qc = useQueryClient();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const uid = useId();

  useEffect(() => { document.title = "Sign in · Yubeos"; }, []);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const me = await login(username.trim(), password);
      setCsrfToken(me.csrf);
      setPassword("");
      qc.setQueryData(ME_KEY, me);
    } catch (err) {
      setError(authErrorMessage(err));
      setPassword("");
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className={styles.page}>
      <form className={styles.card} onSubmit={onSubmit} noValidate>
        <div className={styles.brand}><span className={styles.logo} aria-hidden="true">Y</span>Yubeos</div>
        <h1>Sign in</h1>
        <p className={styles.hint}>Use your Yubeos account (linked to your Linux account).</p>
        <div role="alert" aria-live="assertive">{error && <p className={styles.error}>{error}</p>}</div>
        <div className={styles.field}>
          <label htmlFor={`${uid}-u`}>Username</label>
          <input id={`${uid}-u`} name="username" autoComplete="username" autoCapitalize="none" spellCheck={false}
                 required value={username} onChange={(e) => setUsername(e.target.value)} />
        </div>
        <div className={styles.field}>
          <label htmlFor={`${uid}-p`}>Password</label>
          <input id={`${uid}-p`} name="password" type="password" autoComplete="current-password"
                 required value={password} onChange={(e) => setPassword(e.target.value)} />
        </div>
        <button className={styles.button} type="submit" disabled={busy || !username || !password}>
          {busy ? "Signing in…" : "Sign in"}
        </button>
        <p className={styles.footnote}>First time? Ask the administrator for a setup link (<code>yubeosctl user add</code>).</p>
      </form>
    </main>
  );
}
