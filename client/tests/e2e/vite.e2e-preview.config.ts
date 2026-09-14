// Vite config used exclusively by the Playwright E2E runner to serve the
// production build via `vite preview`.
//
// `vite preview` does NOT read the `server.proxy` block from the main
// `vite.config.ts` — that option only applies to the dev server (`vite dev`).
// Preview has its own, separate `preview.proxy` key. Since the frontend
// resolves API calls as same-origin relative paths when
// `VITE_API_BASE_URL` is unset (see client/src/lib/api-base.ts), this config
// adds exactly that proxy so the browser can reach the E2E backend — which
// listens on a dynamically allocated loopback port picked by the orchestrator
// at run time (client/tests/e2e/lib/orchestrator.mjs) — without any CORS
// headers and without touching the production vite.config.ts.
//
// The backend port is read from E2E_BACKEND_PORT, set by the orchestrator
// right before spawning `vite preview --config` pointed at this file. This
// file is never used outside client/tests/e2e.
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

const backendPort = process.env.E2E_BACKEND_PORT;
if (!backendPort) {
  throw new Error("vite.e2e-preview.config.ts requires E2E_BACKEND_PORT to be set by the E2E orchestrator");
}

export default defineConfig({
  // Resolve root explicitly to client/ regardless of the CLI's invocation
  // cwd, so relative asset/resolve paths behave identically to the main
  // vite.config.ts no matter where `vite --config` is launched from.
  root: fileURLToPath(new URL("../../", import.meta.url)),
  plugins: [react()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("../../src", import.meta.url)) },
  },
  // The orchestrator always passes an explicit absolute --outDir (a per-run
  // temp directory) on both `vite build` and `vite preview`, so this default
  // is only a documented fallback for ad-hoc manual runs; it is gitignored.
  build: { outDir: "dist-e2e", emptyOutDir: true },
  preview: {
    proxy: {
      "/api": {
        target: `http://127.0.0.1:${backendPort}`,
        changeOrigin: true,
        ws: false,
      },
    },
  },
});
