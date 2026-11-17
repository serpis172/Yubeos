import { ApiError } from "../lib/api";

/** Human message for a failed auth call. Problem types are defined in docs/API.md. */
export function authErrorMessage(e: unknown): string {
  if (!(e instanceof ApiError)) return "Something went wrong. Please try again.";
  const code = e.problem.type.split("/").pop();
  switch (code) {
    case "invalid-credentials": return "Incorrect username or password.";
    case "locked-out": return "Too many failed attempts. Wait a few minutes and try again.";
    case "ip-banned": return "Too many failed attempts from this address. Try again later.";
    case "enrollment-required-local": return "A second factor is required. Set one up from a computer on the local network first.";
    case "second-factor-unavailable": return "Second-factor sign-in is not available in this version yet.";
    case "tls-required": return "Sign in over HTTPS.";
    case "priv-unavailable": return "The authentication service is not running on the server.";
    case "invalid-setup-link": return "This setup link is invalid or has expired. Ask the administrator for a new one.";
    case "local-origin-required": return "This link can only be used from a computer on the local network.";
    case "validation": return e.problem.errors?.[0]?.message ?? e.problem.detail ?? "Please check the form.";
    default: return e.status === 0 ? "The server could not be reached." : e.problem.title || "Request failed.";
  }
}
