import { useEffect } from "react";
import { useAddons } from "../addons/useAddons";
import { useT, type Key } from "../i18n";
import { Icon } from "../shell/Icon";
import styles from "./Page.module.css";

/** Lists installed add-ons. "Open" is a plain link to a different origin in a new tab (ADR-0018): no add-on HTML is ever served from Yubeos' origin. */
export function AddonsPage() {
  const t = useT();
  const q = useAddons();
  useEffect(() => { document.title = `${t("addons.title")} · Yubeos`; }, [t]);

  const items = q.data?.items ?? [];
  return (
    <section className={styles.page}>
      <h1>{t("addons.title")}</h1>
      <p className={styles.hint}>{t("addons.intro")}</p>

      {q.isPending && <div className={styles.card} aria-busy="true">{t("addons.loading")}</div>}
      {q.isError && <p className={styles.error} role="alert">{t("addons.loadFailed")}</p>}
      {q.data?.unavailable && <p className={styles.error} role="status">{t("addons.dockerDown")}</p>}

      {q.data && items.length === 0 && (
        <div className={styles.card}><p className={styles.muted}>{t("addons.empty")}</p></div>
      )}

      <ul className={styles.list}>
        {items.map((a) => (
          <li key={a.id} className={styles.item}>
            <Icon name="puzzle" />
            <div className={styles.grow}>
              <strong>{a.name}</strong>
              <span className={styles.muted}>{a.version ? `v${a.version} · ` : ""}{a.description ?? ""}</span>
            </div>
            <span className={`${styles.pill} ${a.state === "running" ? styles.ok : styles.warn}`}>
              {t(`addons.state.${a.state}` as Key)}
            </span>
            {a.url ? (
              <a className={styles.button} href={a.url} target="_blank" rel="noopener noreferrer">{t("addons.open")}</a>
            ) : a.embed !== "none" ? (
              <span className={styles.muted} title={t("addons.blockedHint")}>{t("addons.blocked")}</span>
            ) : null}
          </li>
        ))}
      </ul>
    </section>
  );
}
