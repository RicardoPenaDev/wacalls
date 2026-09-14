import { defineConfig, devices } from "@playwright/test";
import { loadE2EState } from "./tests/e2e/state";

// This config is only ever invoked by client/tests/e2e/run.mjs, which boots
// the backend, GLPI/Tactical mocks and a `vite preview` server first and
// passes their connection details via the E2E_STATE_FILE env var (see
// client/tests/e2e/state.ts). Running `playwright test` directly without
// going through `npm run test:e2e` will fail fast: loadE2EState() throws
// when E2E_STATE_FILE is unset.
//
// Chromium only, managed by Playwright itself (no system browser channel),
// pinned to whatever version ships with the @playwright/test version locked
// in package-lock.json.
const state = loadE2EState();

export default defineConfig({
  testDir: "./tests/e2e",
  testMatch: /.*\.e2e\.spec\.ts/,
  timeout: 45_000,
  expect: { timeout: 10_000 },
  globalTimeout: 240_000,
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [["list"], ["html", { outputFolder: "test-results/html-report", open: "never" }]],
  outputDir: "test-results/artifacts",
  use: {
    baseURL: state.baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    // No Service Worker exists in the SPA today, but this closes the latent
    // gap where one could bypass context.route()'s HTTP(S) interception in
    // the network guard (client/tests/e2e/fixtures.ts).
    serviceWorkers: "block",
    // Needed for the "Copiar número" clipboard assertion
    // (support-workflow.e2e.spec.ts); Chromium headless otherwise blocks
    // navigator.clipboard without an explicit grant.
    permissions: ["clipboard-read", "clipboard-write"],
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
