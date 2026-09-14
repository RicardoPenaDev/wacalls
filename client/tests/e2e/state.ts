// Shared handoff contract between client/tests/e2e/run.mjs (the Node
// orchestrator) and everything Playwright-side (playwright.config.ts,
// fixtures.ts, *.e2e.spec.ts). The orchestrator writes this exact shape as
// JSON to the path in E2E_STATE_FILE before spawning Playwright.
export interface E2EState {
  baseURL: string;
  backendURL: string;
  glpiMockURL: string;
  glpiMockCertPath: string;
  tacticalMockURL: string | null;
  tacticalEnabled: boolean;
  admin: { email: string; password: string };
  operator: { email: string; password: string };
  fixtureDevice: { hostname: string; glpiComputerId: string; tacticalAgentId: string };
  sessionId: string;
  chatJid: string;
  chatJids: string[];
  runDir: string;
  allowedHosts: string[];
}

import fs from "node:fs";

export function loadE2EState(): E2EState {
  const stateFilePath = process.env.E2E_STATE_FILE;
  if (!stateFilePath) {
    throw new Error(
      "E2E_STATE_FILE is not set. Run via `npm --prefix client run test:e2e` (client/tests/e2e/run.mjs), " +
        "never `playwright test` directly — nothing boots the backend/mocks otherwise.",
    );
  }
  return JSON.parse(fs.readFileSync(stateFilePath, "utf8")) as E2EState;
}
