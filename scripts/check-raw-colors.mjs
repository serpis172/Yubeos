// Design-token rule (docs/modules/appearance.md §2): component code must not contain raw colours.
// Only src/theme/*.css (the token definitions) may use hex/rgb()/hsl().
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../web/src", import.meta.url));
const allowed = (p) => p.split(sep).includes("theme") || p.endsWith(".gen.ts") || p.includes(".test.");
// 3/4/6/8-digit hex not preceded by a word char or '&' (HTML entities, issue refs like "(#123)" still match: use words instead)
const pattern = /(?<![\w&])#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3,4})\b|\brgba?\(|\bhsla?\(/;
let bad = 0;

function walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p);
    else if (/\.(css|tsx?)$/.test(name) && !allowed(relative(root, p))) {
      readFileSync(p, "utf8").split("\n").forEach((line, i) => {
        if (pattern.test(line)) {
          console.error(`${relative(root, p)}:${i + 1}: raw colour (use a var(--y-*) token): ${line.trim()}`);
          bad++;
        }
      });
    }
  }
}
walk(root);
if (bad) process.exit(1);
console.log("no-raw-colors: ok");
