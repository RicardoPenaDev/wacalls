#!/usr/bin/env node
// Entry point for `npm --prefix client run test:e2e`.
//
// This is the actual "runner": a plain, multiplatform Node script. It runs
// two sequential phases, each booting its own full E2E environment (mocks,
// backend, built SPA + preview) and running exactly one Playwright spec
// file against it, then unconditionally tearing that environment down —
// success, failure, timeout, or SIGINT/SIGTERM — before moving to the next
// phase. Two phases exist because "support=true, tactical=false" must be a
// genuinely different backend boot (no Tactical mock process, no
// WACALLS_TACTICAL_* env vars at all), not a UI flag layered on top of the
// same backend the other scenarios use.
//
// The ResourceTracker for each phase is created and armed with SIGINT/
// SIGTERM handlers BEFORE bootEnvironment() is ever called, so an interrupt
// during the certgen/go-build/vite-build phase — before any state is
// returned — still has a live tracker to tear down. bootEnvironment() also
// has its own internal try/catch/finally (see lib/orchestrator.mjs) that
// tears down on any boot failure; both call the same idempotent
// tracker.teardown(), so calling it from multiple places is always safe.

import fsp from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";

import { bootEnvironment, clientDir, baseEnv, ResourceTracker, killProcessTree, isWindows } from "./lib/orchestrator.mjs";

function log(msg) {
  process.stdout.write(`[e2e-runner] ${msg}\n`);
}

/** Arms SIGINT/SIGTERM against `tracker`; returns a disarm() to remove the listeners once this phase is done. */
function armSignalHandlers(tracker) {
  let teardownStarted = false;
  const onSignal = (sig) => {
    if (teardownStarted) return;
    teardownStarted = true;
    log(`received ${sig}, tearing down`);
    tracker.teardown(log).finally(() => process.exit(130));
  };
  const sigint = () => onSignal("SIGINT");
  const sigterm = () => onSignal("SIGTERM");
  process.on("SIGINT", sigint);
  process.on("SIGTERM", sigterm);
  return () => {
    process.off("SIGINT", sigint);
    process.off("SIGTERM", sigterm);
  };
}

// Single source of truth for the Playwright CLI child's environment —
// reuses orchestrator.mjs's baseEnv() (never `...process.env`, so no real
// WACALLS_*/credentials from the developer's shell reach it) instead of a
// second, separately-maintained copy that could silently drift from it.
function playwrightEnv(stateFilePath) {
  return { ...baseEnv(), CI: process.env.CI ?? "", E2E_STATE_FILE: stateFilePath };
}

export async function runPhase({
  name,
  withTactical,
  specFiles = [],
  phaseTimeoutMs = 300_000,
  spawnCmd,
  spawnArgs,
}) {
  const tracker = new ResourceTracker();
  const disarm = armSignalHandlers(tracker);
  let stateDir;
  let timer;
  try {
    log(`[${name}] phase start (withTactical=${withTactical})`);
    const { state } = await bootEnvironment({ log, tracker, withTactical });

    stateDir = tracker.trackTempPath(
      await fsp.mkdtemp(path.join(os.tmpdir(), `wacalls-e2e-state-${name}-`)),
    );
    const stateFilePath = path.join(stateDir, "state.json");
    await fsp.writeFile(stateFilePath, JSON.stringify(state, null, 2), "utf8");
    log(`[${name}] environment ready: ${state.baseURL} (state file ${stateFilePath})`);

    const playwrightCli = path.join(clientDir, "node_modules", "@playwright", "test", "cli.js");
    const forwardedArgs = process.argv.slice(2);

    const execCmd = spawnCmd || process.execPath;
    const execArgs = spawnArgs || [playwrightCli, "test", ...specFiles, ...forwardedArgs];

    const { promise, resolve, reject } = Promise.withResolvers();
    let settled = false;
    const safeResolve = (code) => {
      if (!settled) {
        settled = true;
        resolve(code);
      }
    };
    const safeReject = (err) => {
      if (!settled) {
        settled = true;
        reject(err);
      }
    };

    const child = spawn(execCmd, execArgs, {
      cwd: clientDir,
      env: playwrightEnv(stateFilePath),
      stdio: "inherit",
      detached: !isWindows,
    });
    tracker.track(`playwright-${name}`, child);

    timer = setTimeout(async () => {
      log(`[${name}] phase timeout exceeded (${Math.round(phaseTimeoutMs / 1000)}s); killing process tree (pid ${child.pid})`);
      await killProcessTree(child.pid, "SIGKILL").catch(() => {});
      safeReject(new Error(`[${name}] phase timed out after ${Math.round(phaseTimeoutMs / 1000)}s`));
    }, phaseTimeoutMs);

    child.on("error", (err) => {
      clearTimeout(timer);
      safeReject(err);
    });
    child.on("exit", (code) => {
      clearTimeout(timer);
      safeResolve(code ?? 1);
    });

    const exitCode = await promise;
    log(`[${name}] phase finished with exit code ${exitCode}`);
    if (exitCode !== 0) {
      throw new Error(`[${name}] phase failed with exit code ${exitCode}`);
    }
    return exitCode;
  } finally {
    if (timer) clearTimeout(timer);
    log(`[${name}] teardown starting`);
    await tracker.teardown(log);
    log(`[${name}] teardown finished`);
    disarm();
    if (stateDir && (await fsp.stat(stateDir).then(() => true).catch(() => false))) {
      log(`[${name}] warning: stateDir ${stateDir} still exists after teardown`);
    }
  }
}

async function main() {
  const phases = [
    { name: "main", withTactical: true, specFiles: ["support-workflow.e2e.spec.ts", "network-guard.e2e.spec.ts"] },
    { name: "tactical-disabled", withTactical: false, specFiles: ["support-tactical-disabled.e2e.spec.ts"] },
  ];

  let exitCode = 0;
  for (const phase of phases) {
    const code = await runPhase(phase);
    if (code !== 0) exitCode = code;
  }
  process.exit(exitCode);
}

const isMain = process.argv[1] && path.resolve(process.argv[1]) === path.resolve(fileURLToPath(import.meta.url));
if (isMain) {
  main().catch((err) => {
    console.error("[e2e-runner] fatal:", err);
    process.exit(1);
  });
}
