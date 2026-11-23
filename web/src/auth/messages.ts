import { ApiError } from "../lib/api";
import { hasKey, t } from "../i18n";
import { PasskeyError } from "./webauthn";

/** Human message for a failed auth/security call. Problem types are defined in docs/API.md. */
export function authErrorMessage(e: unknown): string {
  if (e instanceof PasskeyError) return e.reason === "unsupported" ? t("auth.passkeyUnsupported") : t("auth.passkeyCancelled");
  if (!(e instanceof ApiError)) return t("err.generic");
  const code = e.problem.type.split("/").pop() ?? "";
  if (code === "validation") return e.problem.errors?.[0]?.message ?? e.problem.detail ?? t("err.generic");
  const key = `err.${code}`;
  if (hasKey(key)) return t(key);
  return e.status === 0 ? t("err.network") : e.problem.title || t("err.generic");
}

/** The problem code of an ApiError, or "". */
export function problemCode(e: unknown): string {
  return e instanceof ApiError ? (e.problem.type.split("/").pop() ?? "") : "";
}
