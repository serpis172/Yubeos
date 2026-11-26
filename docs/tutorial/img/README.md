# Tutorial screenshots

All captures show the real web UI rendered by headless Chromium. `01`–`04`, `06`, `14`, `16`–`18` come from a **real `yubeosd --demo` instance** (mock hardware, real WebSocket). `07`–`13` and `15` (sign-in, enrolment, Security) come from the production build with **simulated API answers** because demo mode has no sign-in; retaking them against a real instance is tracked as [TD-030](../../engineering/TECH-DEBT.md). Themes and language are chosen through the `yubeos.theme` and `yubeos.lang` entries the UI keeps in `localStorage`.

| File | Screen | Theme / language |
|---|---|---|
| `01-shell-blue.png` | Shell with the live Dashboard (demo) | blue / en |
| `02-terminal-placeholder.png` | A section that is specified but not built yet | blue / en |
| `03-theme-light.png`, `04-theme-midnight.png` | Other built-in themes | light, midnight |
| `06-mobile.png` | Narrow viewport | blue / en |
| `07-login.png` | Sign-in | blue / en |
| `08-login-second-factor.png` | Second-factor step | blue / en |
| `09-enroll-choose.png` | Enrolment wizard: choose a factor | blue / en |
| `10-enroll-totp.png` | Enrolment: QR code | blue / en |
| `11-recovery-codes.png` | Enrolment: recovery codes | blue / en |
| `12-security.png` | Settings → Security (admin) | blue / en |
| `13-stepup.png` | Step-up dialog | blue / en |
| `14-appearance-editor.png` | Appearance with the theme editor, language and units (demo) | blue / en |
| `16-dashboard.png` | Dashboard, full page (demo) | blue / en |
| `17-dashboard-mobile.png` | Dashboard on a narrow screen (demo) | blue / en |
| `18-dashboard-it-bits.png` | Dashboard in Italian, bits/s, °F (demo) | midnight / it |
| `15-security-italian.png` | Security page in Italian | blue / it |

Do not edit them by hand. Screenshots must show the real UI, never mock-ups.
