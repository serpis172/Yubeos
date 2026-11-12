// Design-token rule (docs/modules/appearance.md §2): component code must not contain raw colours.
// Only src/theme/*.css (the token definitions) may use hex/rgb()/hsl().
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";

const root = new URL("../web/src", import.meta.url).pathname;
const allowed = (p) => p.split(sep).includes("theme") || p.endsWith(".gen.ts") || p.includes(".test.");
const pattern = /#[0-9a-fA-F]{3,8}\b|\brgba?\(|\bhsla?\(/;
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
