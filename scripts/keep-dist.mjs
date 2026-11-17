// Vite empties dist/ on build; go:embed needs the directory to exist in a fresh clone, so keep a placeholder.
import { mkdirSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
const dist = fileURLToPath(new URL("../web/dist", import.meta.url));
mkdirSync(dist, { recursive: true });
writeFileSync(`${dist}/.gitkeep`, "");
