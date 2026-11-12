// Vite empties dist/ on build; go:embed needs the directory to exist in a fresh clone, so keep a placeholder.
import { writeFileSync, mkdirSync } from "node:fs";
mkdirSync(new URL("../web/dist", import.meta.url), { recursive: true });
writeFileSync(new URL("../web/dist/.gitkeep", import.meta.url), "");
