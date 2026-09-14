// Core process/lifecycle orchestrator for the Playwright E2E suite.
//
// Responsibilities:
// - Allocate loopback-only, dynamic ports for every service (never fixed
//   ports), with retry-on-bind-failure.
// - Generate a throwaway self-signed TLS certificate for the GLPI mock.
// - Build the WACalls server binary once and the SPA once, into a per-run
//   temp directory (never the repo's real dist/ or wacalls.db).
// - Spawn every process directly (no shell, no .cmd/.ps1 shim, no `go run`
//   wrapper for long-running processes) so each has an exact, known PID.
// - Pass each subprocess a minimal, explicit environment — never
//   `...process.env` — so no real WACALLS_GLPI_*/WACALLS_TACTICAL_*
//   credentials or other user environment variables leak into the test.
// - Wait for readiness (no /healthz in the product; GET /api/auth/me
//   answering 401 proves the HTTP server + router are alive, per the
//   approved decision).
// - Track every PID and temp path it creates, and guarantee teardown
//   (SIGTERM each tracked PID, escalate to SIGKILL after a grace period,
//   remove every temp path) on success, failure, timeout, SIGINT, SIGTERM.
//
// This module never touches localhost/127.0.0.1/::1-external addresses: the
// backend's WACALLS_GLPI_BASE_URL / WACALLS_TACTICAL_BASE_URL always point at
// the mocks started in this same process tree.

import { execFile, spawn } from "node:child_process";
import { once } from "node:events";
import fs from "node:fs";
import fsp from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import https from "node:https";
import http from "node:http";
import { promisify } from "node:util";

import { startOnFreePort } from "./ports.mjs";

const execFileAsync = promisify(execFile);

// This file always lives at client/tests/e2e/lib/orchestrator.mjs.
const __dirname = path.dirname(fileURLToPath(import.meta.url));
const e2eDir = path.resolve(__dirname, "..");
const clientDir = path.resolve(e2eDir, "..", "..");
const repoRoot = path.resolve(clientDir, "..");

const isWindows = process.platform === "win32";
const exeSuffix = isWindows ? ".exe" : "";

function resolveGoBin() {
  const toolchainGo = path.resolve(repoRoot, "..", "toolchains", "go1.26.4", "bin", isWindows ? "go.exe" : "go");
  if (fs.existsSync(toolchainGo)) {
    return toolchainGo;
  }
  return isWindows ? "go.exe" : "go";
}

function assertLoopbackURL(rawURL, label) {
  const u = new URL(rawURL);
  const host = u.hostname.toLowerCase();
  if (host !== "127.0.0.1" && host !== "localhost" && host !== "::1" && host !== "[::1]") {
    throw new Error(`[security] ${label} must point strictly to loopback, got ${rawURL}`);
  }
}

// Minimal environment every subprocess gets. Real WACALLS_*
// credentials from the developer's shell are deliberately never inherited.
export function baseEnv() {
  const env = {
    PATH: process.env.PATH ?? "",
    TEMP: process.env.TEMP ?? process.env.TMPDIR ?? "",
    TMP: process.env.TMP ?? process.env.TMPDIR ?? "",
  };

  const toolchainsDir = path.resolve(repoRoot, "..", "toolchains");
  const gocache = path.join(toolchainsDir, "gocache");
  const gopath = path.join(toolchainsDir, "gopath");
  if (fs.existsSync(gocache)) env.GOCACHE = gocache;
  if (fs.existsSync(gopath)) env.GOPATH = gopath;

  if (isWindows) {
    env.SystemRoot = process.env.SystemRoot ?? "C:\\Windows";
    env.PATHEXT = process.env.PATHEXT ?? ".COM;.EXE;.BAT;.CMD";
    env.USERPROFILE = process.env.USERPROFILE ?? "";
    env.APPDATA = process.env.APPDATA ?? "";
    env.LOCALAPPDATA = process.env.LOCALAPPDATA ?? "";
  } else {
    env.HOME = process.env.HOME ?? "";
  }
  return env;
}

function waitForHttp(url, { timeoutMs = 30_000, acceptStatus, ca } = {}) {
  const deadline = Date.now() + timeoutMs;
  const mod = url.startsWith("https:") ? https : http;
  const { promise, resolve, reject } = Promise.withResolvers();
  const attempt = () => {
    const req = mod.get(url, ca ? { ca, rejectUnauthorized: true } : {}, (res) => {
      res.resume();
      const ok = acceptStatus ? acceptStatus(res.statusCode) : res.statusCode >= 200 && res.statusCode < 500;
      if (ok) {
        resolve();
      } else if (Date.now() > deadline) {
        reject(new Error(`waitForHttp: ${url} never became ready (last status ${res.statusCode})`));
      } else {
        setTimeout(attempt, 200);
      }
    });
    req.on("error", () => {
      if (Date.now() > deadline) reject(new Error(`waitForHttp: ${url} never became reachable`));
      else setTimeout(attempt, 200);
    });
  };
  attempt();
  return promise;
}

// killProcessTree terminates exactly the process this runner spawned, and
// only its own descendants — never a process by name, never a broad filter,
// never anything this runner did not create itself.
//
// Windows: there is no SIGTERM/SIGKILL distinction for arbitrary processes
// and no native way from Node to signal a whole process group, so
// `taskkill /PID <exact-pid> /T /F` is used unconditionally — it terminates
// the exact PID and its descendants immediately. `/IM` (image name) and any
// name-based or wildcard invocation of taskkill are forbidden anywhere in
// this file.
//
// Unix (Linux/macOS): every long-running child in this file is spawned with
// `detached: true`, which makes it the leader of its own new process group
// (its pgid equals its own pid). Signaling the *negative* pid
// (`process.kill(-pid, signal)`) delivers the signal to that entire group —
// the child and anything it spawned — while never touching this runner's
// own process group or any unrelated process. SIGTERM is tried first and
// escalated to SIGKILL only after a grace period, matching real POSIX
// semantics (unlike Windows, where every kill is already an immediate hard
// termination of the whole tree).
export async function killProcessTree(pid, signal) {
  if (isWindows) {
    try {
      await execFileAsync("taskkill", ["/PID", String(pid), "/T", "/F"]);
    } catch (err) {
      const msg = String(err?.message ?? err);
      // Exit code 128 ("not found") means the process already exited —
      // idempotent no-op, not a failure.
      if (!/not found|128/i.test(msg)) throw err;
    }
    return;
  }
  try {
    process.kill(-pid, signal);
  } catch (err) {
    if (err?.code === "ESRCH") return;
    try {
      process.kill(pid, signal);
    } catch (err2) {
      if (err2?.code === "ESRCH") return;
      throw err2;
    }
  }
}

/**
 * Tracks every spawned child and every temp path so teardown is exhaustive.
 * `teardown()` is idempotent and safe to call from multiple independent
 * places (a boot-failure catch, a SIGINT handler, a normal-exit finally)
 * concurrently or repeatedly — every call after the first just awaits the
 * same in-flight/completed teardown.
 */
export class ResourceTracker {
  constructor() {
    this.children = []; // { name, child }
    this.tempPaths = [];
    this._teardownPromise = null;
  }

  track(name, child) {
    this.children.push({ name, child });
    return child;
  }

  trackTempPath(p) {
    this.tempPaths.push(p);
    return p;
  }

  teardown(log) {
    if (!this._teardownPromise) {
      this._teardownPromise = this._teardownOnce(log);
    }
    return this._teardownPromise;
  }

  async _teardownOnce(log) {
    // Reverse order: stop consumers (browser already closed by caller) before
    // producers, and kill newest-spawned first.
    for (const { name, child } of [...this.children].reverse()) {
      if (child.exitCode !== null || child.signalCode !== null) continue;
      log?.(`teardown: stopping ${name} (pid ${child.pid})`);
      if (isWindows) {
        await killProcessTree(child.pid, null).catch((err) => log?.(`teardown: ${name} taskkill failed: ${err.message}`));
        await Promise.race([once(child, "exit"), new Promise((r) => setTimeout(r, 3000))]);
        continue;
      }
      await killProcessTree(child.pid, "SIGTERM").catch((err) => log?.(`teardown: ${name} SIGTERM failed: ${err.message}`));
      const exited = await Promise.race([
        once(child, "exit").then(() => true),
        new Promise((r) => setTimeout(() => r(false), 5000)),
      ]);
      if (!exited) {
        log?.(`teardown: ${name} (pid ${child.pid}) ignored SIGTERM, sending SIGKILL to its process group`);
        await killProcessTree(child.pid, "SIGKILL").catch((err) => log?.(`teardown: ${name} SIGKILL failed: ${err.message}`));
        await Promise.race([once(child, "exit"), new Promise((r) => setTimeout(r, 3000))]);
      }
    }
    for (const p of [...this.tempPaths].reverse()) {
      await fsp.rm(p, { recursive: true, force: true }).catch(() => {});
    }
  }
}

function synthCred(label) {
  return `e2e-${label}-${Math.random().toString(36).slice(2, 10)}`;
}

/**
 * Boots the whole E2E environment: certs, mocks, backend, built+previewed
 * frontend. Returns { state, tracker } where `state` is the plain JSON-able
 * object handed to the Playwright process, and `tracker` exposes
 * `teardown()`.
 */
export async function bootEnvironment({ log = console.log, tracker = new ResourceTracker(), withTactical = true, failAt } = {}) {
  try {
    const runDir = tracker.trackTempPath(await fsp.mkdtemp(path.join(os.tmpdir(), "wacalls-e2e-")));
    const certDir = path.join(runDir, "certs");
    const distDir = path.join(runDir, "dist");
    const dbPath = path.join(runDir, "wacalls-e2e-server.db");
    const serverBinPath = path.join(runDir, `wacalls-e2e-server${exeSuffix}`);

    const go = resolveGoBin();

    // 1. Self-signed certificate for the GLPI mock (client/tests/e2e/lib -> repoRoot via cmd/e2egencert).
    log("generating throwaway TLS certificate for the GLPI mock");
    await runToCompletion(go, ["run", "./cmd/e2egencert", certDir], { cwd: repoRoot, env: baseEnv() }, tracker, "certgen");
    const certPath = path.join(certDir, "cert.pem");
    const keyPath = path.join(certDir, "key.pem");
    const certPem = await fsp.readFile(certPath);

    // 2. Build the server binary once.
    log("building WACalls server binary for E2E");
    await runToCompletion(go, ["build", "-o", serverBinPath, "./cmd/server"], { cwd: repoRoot, env: baseEnv() }, tracker, "go-build");

    // 3. Build the SPA once into the per-run temp dist dir.
    log("building SPA for E2E");
    await runToCompletion(
      process.execPath,
      [require_vite_bin(), "build", "--outDir", distDir, "--emptyOutDir"],
      { cwd: clientDir, env: { ...baseEnv(), NODE_ENV: "production" } },
      tracker,
      "vite-build",
    );

    // Test-only fault injection (client/tests/e2e/_verify-cleanup.mjs) —
    // never set by run.mjs in normal operation. Proves teardown() cleans up
    // a boot that fails right after the build stage (certgen + go-build +
    // vite-build already completed and are tracked, no long-running child
    // yet).
    if (failAt === "build") {
      throw new Error("[fault-injection] simulated failure right after the build stage");
    }

    const glpiCreds = {
      clientId: synthCred("client-id"),
      clientSecret: synthCred("client-secret"),
      username: synthCred("user"),
      password: synthCred("pass"),
    };
    const tacticalApiKey = synthCred("tactical-key");
    const adminEmail = `admin-${Math.random().toString(36).slice(2, 8)}@e2e.invalid`;
    const adminPassword = synthCred("admin-pw");
    const fixtureDevice = {
      hostname: "E2E-PC-01",
      glpiComputerId: "501",
      tacticalAgentId: "agent-e2e-01",
    };

    // 4. GLPI mock (HTTPS, loopback-only).
    const glpiMock = await startOnFreePort(async (port) => {
      const child = spawn(
        process.execPath,
        [
          path.join(e2eDir, "mocks", "glpi-mock.mjs"),
          String(port),
          certPath,
          keyPath,
          glpiCreds.clientId,
          glpiCreds.clientSecret,
          glpiCreds.username,
          glpiCreds.password,
        ],
        { cwd: e2eDir, env: baseEnv(), stdio: ["ignore", "pipe", "pipe"], detached: !isWindows },
      );
      tracker.track("glpi-mock", child);
      pipeChildLogs(child, "glpi-mock", log);
      try {
        await waitForHttp(`https://127.0.0.1:${port}/health`, { timeoutMs: 8000, ca: certPem });
      } catch (err) {
        err.retryable = true;
        throw err;
      }
      return { port, child };
    });

    // 5. Tactical mock (plain HTTP, loopback-only) — skipped entirely when
    // withTactical is false, for the dedicated "support=true, tactical=false"
    // real-boot scenario: no mock process, no WACALLS_TACTICAL_* env vars
    // passed to the backend below, so the server genuinely runs with the
    // Tactical integration disabled (supCfg.TacticalConfig == nil), not just
    // a UI feature flag pretending it is off.
    const tacticalMock = withTactical
      ? await startOnFreePort(async (port) => {
          const child = spawn(process.execPath, [path.join(e2eDir, "mocks", "tactical-mock.mjs"), String(port), tacticalApiKey], {
            cwd: e2eDir,
            env: baseEnv(),
            stdio: ["ignore", "pipe", "pipe"],
            detached: !isWindows,
          });
          tracker.track("tactical-mock", child);
          pipeChildLogs(child, "tactical-mock", log);
          try {
            await waitForHttp(`http://127.0.0.1:${port}/health`, { timeoutMs: 8000 });
          } catch (err) {
            err.retryable = true;
            throw err;
          }
          return { port, child };
        })
      : null;

    // 6. WACalls backend.
    const backend = await startOnFreePort(async (port) => {
      const glpiOrigin = `https://127.0.0.1:${glpiMock.port}`;
      assertLoopbackURL(glpiOrigin, "WACALLS_GLPI_BASE_URL");
      assertLoopbackURL(glpiOrigin, "WACALLS_GLPI_WEB_BASE_URL");

      const env = {
        ...baseEnv(),
        WACALLS_SUPPORT_ENABLED: "1",
        WACALLS_GLPI_BASE_URL: glpiOrigin,
        WACALLS_GLPI_WEB_BASE_URL: glpiOrigin,
        WACALLS_GLPI_CLIENT_ID: glpiCreds.clientId,
        WACALLS_GLPI_CLIENT_SECRET: glpiCreds.clientSecret,
        WACALLS_GLPI_USERNAME: glpiCreds.username,
        WACALLS_GLPI_PASSWORD: glpiCreds.password,
        WACALLS_GLPI_CA_FILE: certPath,
      };
      if (tacticalMock) {
        const tacticalOrigin = `http://127.0.0.1:${tacticalMock.port}`;
        assertLoopbackURL(tacticalOrigin, "WACALLS_TACTICAL_BASE_URL");
        env.WACALLS_TACTICAL_BASE_URL = tacticalOrigin;
        env.WACALLS_TACTICAL_API_KEY = tacticalApiKey;
      }
      const child = spawn(
        serverBinPath,
        [
        "-e2e-mode",
        "-e2e-run-dir",
        runDir,
        "-addr",
          `127.0.0.1:${port}`,
          "-db",
          dbPath,
          "-static",
          distDir,
          "-seed-admin-email",
          adminEmail,
          "-seed-admin-password",
          adminPassword,
          "-e2e-session-id",
          "e2e-session-01",
          "-e2e-session-name",
          "E2E WhatsApp",
          "-e2e-own-jid",
          "5511999990000@s.whatsapp.net",
          "-e2e-chat-jid",
          "5511999990001@s.whatsapp.net",
          "-e2e-chat-name",
          "E2E Customer",
          "-e2e-hostname",
          fixtureDevice.hostname,
          "-e2e-glpi-computer-id",
          fixtureDevice.glpiComputerId,
          "-e2e-tactical-agent-id",
          tacticalMock ? fixtureDevice.tacticalAgentId : "",
        ],
        { cwd: runDir, env, stdio: ["ignore", "pipe", "pipe"], detached: !isWindows },
      );
      tracker.track("backend", child);
      pipeChildLogs(child, "backend", log);
      // Test-only fault injection: simulates the backend never becoming
      // ready, with glpi-mock/tactical-mock already running.
      if (failAt === "backend-readiness") {
        throw new Error("[fault-injection] simulated backend readiness failure");
      }
      try {
        await waitForHttp(`http://127.0.0.1:${port}/api/auth/me`, {
          timeoutMs: 30_000,
          acceptStatus: (code) => code === 401 || code === 200,
        });
      } catch (err) {
        err.retryable = true;
        throw err;
      }
      return { port, child };
    });

    // 6b. Seed GLPI mock computer (+ Tactical mock agent, when enabled) fixtures.
    log("seeding GLPI mock computer fixture" + (tacticalMock ? " + Tactical mock agent fixture" : " (Tactical mock disabled for this boot)"));
    await httpsPostJSON(`https://127.0.0.1:${glpiMock.port}/__e2e__/computers`, certPem, [
      { id: fixtureDevice.glpiComputerId, name: fixtureDevice.hostname, serial: "SN-E2E-001", entity: { id: "1", name: "E2E Entity" } },
    ]);
    if (tacticalMock) {
      await httpPostJSON(`http://127.0.0.1:${tacticalMock.port}/__e2e__/agents`, [
        {
          agent_id: fixtureDevice.tacticalAgentId,
          hostname: fixtureDevice.hostname,
          client: "E2E Client",
          site_name: "E2E Site",
          site: 1,
          status: "online",
          last_seen: new Date().toISOString(),
          monitoring_type: "workstation",
          operating_system: "Windows 11",
          logged_in_username: "e2e-user",
          last_logged_in_user: "e2e-user",
          local_ips: ["127.0.0.1"],
          serial_number: "SN-E2E-001",
          needs_reboot: false,
          maintenance_mode: false,
          version: "1.0.0",
          plat: "windows",
        },
      ]);
    }

    log("creating a non-admin operator user for permission-boundary scenarios");
    const operatorEmail = `operator-${Math.random().toString(36).slice(2, 8)}@e2e.invalid`;
    const operatorPassword = synthCred("operator-pw");
    await createOperatorUser(backend.port, { email: adminEmail, password: adminPassword }, { email: operatorEmail, password: operatorPassword }, "e2e-session-01");

    // 7. Preview server for the built SPA, proxying /api to the backend.
    const preview = await startOnFreePort(async (port) => {
      const child = spawn(
        process.execPath,
        [
          require_vite_bin(),
          "preview",
          "--config",
          path.join(e2eDir, "vite.e2e-preview.config.ts"),
          "--outDir",
          distDir,
          "--host",
          "127.0.0.1",
          "--port",
          String(port),
          "--strictPort",
        ],
        {
          cwd: clientDir,
          env: { ...baseEnv(), E2E_BACKEND_PORT: String(backend.port) },
          stdio: ["ignore", "pipe", "pipe"],
          detached: !isWindows,
        },
      );
      tracker.track("preview", child);
      pipeChildLogs(child, "preview", log);
      // Test-only fault injection: simulates the preview server never
      // becoming ready, with every other service already running.
      if (failAt === "preview-readiness") {
        throw new Error("[fault-injection] simulated preview readiness failure");
      }
      try {
        await waitForHttp(`http://127.0.0.1:${port}/`, { timeoutMs: 15_000 });
      } catch (err) {
        err.retryable = true;
        throw err;
      }
      return { port, child };
    });

    const state = {
      baseURL: `http://127.0.0.1:${preview.port}`,
      backendURL: `http://127.0.0.1:${backend.port}`,
      glpiMockURL: `https://127.0.0.1:${glpiMock.port}`,
      glpiMockCertPath: certPath,
      tacticalMockURL: tacticalMock ? `http://127.0.0.1:${tacticalMock.port}` : null,
      tacticalEnabled: !!tacticalMock,
      admin: { email: adminEmail, password: adminPassword },
      operator: { email: operatorEmail, password: operatorPassword },
      fixtureDevice,
      sessionId: "e2e-session-01",
      chatJid: "5511999990001@s.whatsapp.net",
      chatJids: [
        "5511999990001@s.whatsapp.net",
        "5511999990002@s.whatsapp.net",
        "5511999990003@s.whatsapp.net",
        "5511999990004@s.whatsapp.net",
        "5511999990005@s.whatsapp.net",
        "5511999990006@s.whatsapp.net",
        "5511999990007@s.whatsapp.net",
        "5511999990008@s.whatsapp.net",
      ],
      runDir,
      allowedHosts: ["127.0.0.1", "localhost", "[::1]", "::1"],
    };

    return { state, tracker };
  } catch (err) {
    log?.(`bootEnvironment failed (${err?.message ?? err}); tearing down partially-started resources`);
    try {
      await tracker.teardown(log);
    } catch (teardownErr) {
      log?.(`teardown after boot failure also failed: ${teardownErr.message}`);
      err.cleanupError = teardownErr;
    }
    throw err;
  }
}

/**
 * Logs in as admin, creates a non-admin operator user, using plain HTTP
 * requests against the backend directly (never through the browser). The
 * backend is loopback-only HTTP in this environment, so no TLS handling is
 * needed here.
 */
function createOperatorUser(backendPort, adminCreds, operatorCreds, sessionId) {
  const { promise, resolve, reject } = Promise.withResolvers();
  const loginPayload = Buffer.from(JSON.stringify({ email: adminCreds.email, password: adminCreds.password }));
  const loginReq = http.request(
    { host: "127.0.0.1", port: backendPort, path: "/api/auth/login", method: "POST", headers: { "Content-Type": "application/json", "Content-Length": loginPayload.length } },
    (loginRes) => {
      const setCookie = loginRes.headers["set-cookie"];
      loginRes.resume();
      loginRes.on("end", () => {
        if (loginRes.statusCode !== 200 || !setCookie || setCookie.length === 0) {
          reject(new Error(`admin login failed: status ${loginRes.statusCode}`));
          return;
        }
        const cookie = setCookie[0].split(";")[0];
        const createPayload = Buffer.from(
          JSON.stringify({ email: operatorCreds.email, password: operatorCreds.password, name: "E2E Operator", role: "" }),
        );
        const createReq = http.request(
          {
            host: "127.0.0.1",
            port: backendPort,
            path: "/api/users",
            method: "POST",
            headers: { "Content-Type": "application/json", "Content-Length": createPayload.length, Cookie: cookie },
          },
          (createRes) => {
            let body = "";
            createRes.on("data", (d) => (body += d));
            createRes.on("end", () => {
              if (createRes.statusCode !== 200) {
                reject(new Error(`operator creation failed: status ${createRes.statusCode}: ${body}`));
                return;
              }
              let parsed;
              try {
                parsed = JSON.parse(body);
              } catch (e) {
                reject(new Error(`failed to parse create user response: ${e.message}`));
                return;
              }
              const userId = parsed?.user?.id;
              if (!userId || !sessionId) {
                resolve();
                return;
              }
              // Link session to the operator
              const linkPayload = Buffer.from(JSON.stringify({ sessionIds: [sessionId] }));
              const linkReq = http.request(
                {
                  host: "127.0.0.1",
                  port: backendPort,
                  path: `/api/users/${encodeURIComponent(userId)}/sessions`,
                  method: "PUT",
                  headers: { "Content-Type": "application/json", "Content-Length": linkPayload.length, Cookie: cookie },
                },
                (linkRes) => {
                  linkRes.resume();
                  linkRes.on("end", () => {
                    if (linkRes.statusCode >= 200 && linkRes.statusCode < 300) resolve();
                    else reject(new Error(`operator session linking failed: status ${linkRes.statusCode}`));
                  });
                },
              );
              linkReq.on("error", reject);
              linkReq.end(linkPayload);
            });
          },
        );
        createReq.on("error", reject);
        createReq.end(createPayload);
      });
    },
  );
  loginReq.on("error", reject);
  loginReq.end(loginPayload);
  return promise;
}

function require_vite_bin() {
  // Resolve vite's JS entry directly (never node_modules/.bin/vite(.cmd)) so
  // we always spawn a plain `node <script>` process with an exact PID and no
  // shell/shim indirection on any platform.
  return path.join(clientDir, "node_modules", "vite", "bin", "vite.js");
}

function httpsPostJSON(url, ca, body) {
  const { promise, resolve, reject } = Promise.withResolvers();
  const payload = Buffer.from(JSON.stringify(body));
  const req = https.request(
    url,
    { method: "POST", ca, headers: { "Content-Type": "application/json", "Content-Length": payload.length } },
    (res) => {
      res.resume();
      res.on("end", () => (res.statusCode && res.statusCode < 400 ? resolve() : reject(new Error(`POST ${url} -> ${res.statusCode}`))));
    },
  );
  req.on("error", reject);
  req.end(payload);
  return promise;
}

function httpPostJSON(url, body) {
  const { promise, resolve, reject } = Promise.withResolvers();
  const payload = Buffer.from(JSON.stringify(body));
  const req = http.request(
    url,
    { method: "POST", headers: { "Content-Type": "application/json", "Content-Length": payload.length } },
    (res) => {
      res.resume();
      res.on("end", () => (res.statusCode && res.statusCode < 400 ? resolve() : reject(new Error(`POST ${url} -> ${res.statusCode}`))));
    },
  );
  req.on("error", reject);
  req.end(payload);
  return promise;
}

function pipeChildLogs(child, name, log) {
  child.stdout?.on("data", (d) => log(`[${name}] ${d.toString().trimEnd()}`));
  child.stderr?.on("data", (d) => log(`[${name}] ${d.toString().trimEnd()}`));
}

// runToCompletion runs a short-lived build/codegen step to completion.
// When `tracker`/`name` are supplied, the child is tracked from the instant
// it is spawned (before this function's promise settles), so a SIGINT/
// teardown that lands while `go build`/`go run`/`vite build` is still
// in-flight kills it (and, via killProcessTree, its own descendants) —
// closing the gap where the certgen/build phase used to be untracked and
// could survive an interrupted boot.
function runToCompletion(cmd, args, opts, tracker, name) {
  const { promise, resolve, reject } = Promise.withResolvers();
  const child = spawn(cmd, args, { ...opts, stdio: ["ignore", "pipe", "pipe"], detached: !isWindows });
  if (tracker && name) tracker.track(name, child);
  let out = "";
  let errOut = "";
  child.stdout?.on("data", (d) => (out += d));
  child.stderr?.on("data", (d) => (errOut += d));
  child.on("error", reject);
  child.on("exit", (code) => {
    if (code === 0) resolve({ stdout: out, stderr: errOut });
    else reject(new Error(`${cmd} ${args.join(" ")} exited with ${code}\n${errOut || out}`));
  });
  return promise;
}

export { repoRoot, clientDir, e2eDir, isWindows };
