// Runs before first paint (blocking, same-origin => allowed by CSP script-src 'self'): applies the saved theme
// so users of the light/system themes never see a flash of the default dark one. Keep in sync with Shell.tsx.
(function () {
  var theme = "system";
  try { theme = localStorage.getItem("yubeos.theme") || "system"; } catch {}
  var resolved = theme;
  if (theme === "system") {
    var light = window.matchMedia && window.matchMedia("(prefers-color-scheme: light)").matches;
    resolved = light ? "yubeos-light" : "yubeos-blue";
  }
  document.documentElement.setAttribute("data-theme", resolved);
})();
