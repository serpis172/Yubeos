import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

// Dev: `make demo` runs yubeosd --demo --no-tls on :8443; Vite proxies API + WebSockets to it.
const backend = process.env.YUBEOS_BACKEND ?? "http://localhost:8443";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target: backend, changeOrigin: false },
      "/ws": { target: backend, ws: true },
      "/healthz": { target: backend },
    },
  },
  build: { outDir: "dist", emptyOutDir: true, sourcemap: false, target: "es2022" },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
  },
});
