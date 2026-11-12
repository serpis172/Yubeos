import { useLocation } from "react-router-dom";
import { allNavItems } from "../shell/nav";
import styles from "../shell/Shell.module.css";

/** Stand-in for sections that are specified but not yet built (see docs/ROADMAP.md). */
export function Placeholder() {
  const { pathname } = useLocation();
  const item = allNavItems.find((i) => i.to === pathname);
  return (
    <section className={styles.card}>
      <h1>{item?.label ?? "Not found"}</h1>
      {item ? (
        <>
          <p>This section is specified in the documentation and scheduled for <span className={styles.badge}>Phase {item.phase}</span>.</p>
          <p>Nothing to configure yet.</p>
        </>
      ) : (
        <p>This page does not exist.</p>
      )}
    </section>
  );
}
