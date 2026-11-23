// Runs before first paint (blocking, same-origin => allowed by CSP script-src 'self'): applies the saved theme
// so users of the light/system/custom themes never see a flash of the default dark one.
// Keep in sync with src/theme/engine.ts (resolveTheme, themeVars).
(function () {
  var root = document.documentElement;
  var choice = "system";
  try { choice = localStorage.getItem("yubeos.theme") || "system"; } catch {}
  var resolved = choice;
  if (choice === "system") {
    var light = window.matchMedia && window.matchMedia("(prefers-color-scheme: light)").matches;
    resolved = light ? "yubeos-light" : "yubeos-blue";
  }
  if (choice.indexOf("custom:") === 0) {
    try {
      var list = JSON.parse(localStorage.getItem("yubeos.themes") || "[]");
      var t = list.filter(function (x) { return x && x.id === choice.slice(7); })[0];
      if (t && t.colors && t.typography) {
        var names = { bg: "--y-bg", surface1: "--y-surface-1", surface2: "--y-surface-2", border: "--y-border", text: "--y-text", textMuted: "--y-text-muted",
          accent: "--y-accent", accentContrast: "--y-accent-contrast", success: "--y-success", warning: "--y-warning", danger: "--y-danger", info: "--y-info" };
        for (var k in names) if (typeof t.colors[k] === "string") root.style.setProperty(names[k], t.colors[k]);
        root.style.setProperty("--y-font-ui", t.typography.ui);
        root.style.setProperty("--y-font-mono", t.typography.mono);
        if (t.typography.baseSizePx) root.style.setProperty("--y-font-size", t.typography.baseSizePx + "px");
        if (t.shape && t.shape.radiusPx !== undefined) root.style.setProperty("--y-radius", t.shape.radiusPx + "px");
        resolved = t.base === "light" ? "yubeos-light" : "yubeos-blue";
      } else { resolved = "yubeos-blue"; }
    } catch { resolved = "yubeos-blue"; }
  }
  root.setAttribute("data-theme", resolved);
})();
