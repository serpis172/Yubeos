import { useEffect, useSyncExternalStore } from "react";
import { applyChoice, getThemeState, setChoice, subscribeTheme } from "./engine";

/** Current theme choice + custom themes; applies the theme to <html> and follows the OS when the choice is "system". */
export function useTheme() {
  const s = useSyncExternalStore(subscribeTheme, getThemeState);
  useEffect(() => {
    const mq = window.matchMedia?.("(prefers-color-scheme: light)");
    const apply = () => applyChoice(s.choice, !!mq?.matches, s.custom);
    apply();
    if (s.choice === "system") {
      mq?.addEventListener?.("change", apply);
      return () => mq?.removeEventListener?.("change", apply);
    }
  }, [s]);
  return { choice: s.choice, custom: s.custom, setChoice };
}
