// Shared Playwright fixtures for the WACalls support E2E suite:
// - `state`: the environment handed off by run.mjs (ports, creds, fixtures).
// - network allowlist: every HTTP(S) request AND every WebSocket connection
//   from every page/context in every test is intercepted; anything that
//   isn't 127.0.0.1/localhost/::1 aborts (never reaches the real network —
//   Playwright's routing intercepts before DNS/connect) AND fails the test
//   immediately (per the approved requirement that the suite must never
//   reach a real host). `networkOffenders` exposes the recorded list so
//   network-guard.e2e.spec.ts can assert the guard itself actually catches
//   an attempt, without depending on real internet/DNS for the probe host.
// - Service workers are disabled for every context (`serviceWorkers:
//   "block"` in playwright.config.ts) — the SPA does not register one
//   today, but this closes the latent gap where a future one could bypass
//   `context.route()`.
// - `loginAs`: drives the real login form (no direct cookie injection) so
//   the auth cookie flows exactly as a user's browser would produce it.
// - `glpiControl` / `tacticalControl`: thin clients for the mocks' /__e2e__
//   control endpoints, used to script scenarios (429, ambiguous 5xx, etc).
import { test as base, expect, type Page } from "@playwright/test";
import fs from "node:fs";
import https from "node:https";
import http from "node:http";
import { loadE2EState, type E2EState } from "./state";

export const state: E2EState = loadE2EState();
const glpiCaCert = fs.readFileSync(state.glpiMockCertPath);

export function isAllowedHost(hostname: string): boolean {
  return hostname === "127.0.0.1" || hostname === "localhost" || hostname === "::1";
}

type Fixtures = {
  networkOffenders: string[];
  networkGuard: void;
};

export const test = base.extend<Fixtures>({
  // Not auto — a plain array a test can read/clear. Declared separately from
  // networkGuard so network-guard.e2e.spec.ts can request it directly and
  // assert on (then clear) an intentionally-triggered offender without
  // tripping the auto-fixture's own end-of-test assertion below.
  networkOffenders: [
    // eslint-disable-next-line no-empty-pattern
    async ({}, use) => {
      const offenders: string[] = [];
      await use(offenders);
    },
    { auto: false },
  ],

  networkGuard: [
    async ({ context, networkOffenders }, use) => {
      await context.route("**/*", (route) => {
        let hostname: string;
        try {
          hostname = new URL(route.request().url()).hostname;
        } catch {
          route.abort("blockedbyclient");
          return;
        }
        if (!isAllowedHost(hostname)) {
          networkOffenders.push(route.request().url());
          route.abort("blockedbyclient");
          return;
        }
        route.continue();
      });

      // WebSocket connections are a separate interception surface in
      // Playwright (context.route() does not cover them). Loopback sockets
      // are proxied through to the real server via connectToServer();
      // anything else is closed immediately without ever connecting out.
      await context.routeWebSocket("**/*", (ws) => {
        let hostname: string;
        try {
          hostname = new URL(ws.url()).hostname;
        } catch {
          ws.close({ code: 1008, reason: "network-guard: unparsable WebSocket URL" });
          return;
        }
        if (!isAllowedHost(hostname)) {
          networkOffenders.push(ws.url());
          ws.close({ code: 1008, reason: "network-guard: non-loopback WebSocket blocked" });
          return;
        }
        ws.connectToServer();
      });

      await use();
      expect(networkOffenders, `request(s) left the loopback allowlist: ${networkOffenders.join(", ")}`).toEqual([]);
    },
    { auto: true },
  ],
});

export { expect };

/** Drives the real /login form; never injects a cookie directly. */
export async function loginAs(page: Page, creds: { email: string; password: string }) {
  await page.goto("/login");
  await page.locator("#email").fill(creds.email);
  await page.locator("#password").fill(creds.password);
  await page.getByRole("button", { name: /entrar/i }).click();
  await page.waitForURL(/\/chats/);
}

function httpsPostJSON(baseUrl: string, path: string, body: unknown): Promise<void> {
  let settled = false;
  const { promise, resolve, reject } = Promise.withResolvers<void>();
  const safeResolve = () => {
    if (!settled) {
      settled = true;
      resolve();
    }
  };
  const safeReject = (err: Error) => {
    if (!settled) {
      settled = true;
      reject(err);
    }
  };

  const url = new URL(path, baseUrl);
  const payload = Buffer.from(JSON.stringify(body ?? {}));
  const req = https.request(
    url,
    {
      method: "POST",
      agent: false,
      ca: glpiCaCert,
      headers: {
        "Content-Type": "application/json",
        "Content-Length": payload.length,
        Connection: "close",
      },
    },
    (res) => {
      res.resume();
      res.on("end", () => {
        if (res.statusCode && res.statusCode < 400) safeResolve();
        else safeReject(new Error(`${path} -> ${res.statusCode}`));
      });
    },
  );
  req.setTimeout(5000, () => {
    req.destroy(new Error(`httpsPostJSON: timeout after 5s on ${path}`));
    safeReject(new Error(`httpsPostJSON: timeout after 5s on ${path}`));
  });
  req.on("error", safeReject);
  req.end(payload);
  return promise;
}

function httpsGetJSON<T>(baseUrl: string, path: string): Promise<T> {
  let settled = false;
  const { promise, resolve, reject } = Promise.withResolvers<T>();
  const safeResolve = (val: T) => {
    if (!settled) {
      settled = true;
      resolve(val);
    }
  };
  const safeReject = (err: Error) => {
    if (!settled) {
      settled = true;
      reject(err);
    }
  };

  const url = new URL(path, baseUrl);
  const req = https.get(
    url,
    {
      agent: false,
      ca: glpiCaCert,
      headers: {
        Connection: "close",
      },
    },
    (res) => {
      let body = "";
      res.on("data", (d) => (body += d));
      res.on("end", () => {
        if (res.statusCode && res.statusCode < 400) {
          try {
            safeResolve(JSON.parse(body));
          } catch (e: any) {
            safeReject(new Error(`failed to parse JSON from ${path}: ${e.message}`));
          }
        } else {
          safeReject(new Error(`${path} -> ${res.statusCode}`));
        }
      });
    },
  );
  req.setTimeout(5000, () => {
    req.destroy(new Error(`httpsGetJSON: timeout after 5s on ${path}`));
    safeReject(new Error(`httpsGetJSON: timeout after 5s on ${path}`));
  });
  req.on("error", safeReject);
  return promise;
}

export const glpiControl = {
  reset: () => httpsPostJSON(state.glpiMockURL, "/__e2e__/reset", {}),
  setNextTicketResponse: (mode: "success" | "429" | "5xx" | "malformed" | "failed" | "400", retryAfterSeconds?: number) =>
    httpsPostJSON(state.glpiMockURL, "/__e2e__/next-ticket-response", { mode, retryAfterSeconds }),
  lastAmbiguousTicket: () => httpsGetJSON<{ id: string | null }>(state.glpiMockURL, "/__e2e__/last-ambiguous-ticket"),
  callCounts: () => httpsGetJSON<Record<string, number>>(state.glpiMockURL, "/__e2e__/call-counts"),
  // Lets a test observe the "processing" state (already persisted in
  // WACalls' DB by the atomic create+claim) before GLPI answers: the
  // ticket-create HTTP call simply hangs until releaseHeldTicket() is
  // called.
  holdNextTicket: () => httpsPostJSON(state.glpiMockURL, "/__e2e__/hold-next-ticket", {}),
  releaseHeldTicket: () => httpsPostJSON(state.glpiMockURL, "/__e2e__/release-held-ticket", {}),
};

function httpPostJSON(baseUrl: string, path: string, body: unknown): Promise<void> {
  let settled = false;
  const { promise, resolve, reject } = Promise.withResolvers<void>();
  const safeResolve = () => {
    if (!settled) {
      settled = true;
      resolve();
    }
  };
  const safeReject = (err: Error) => {
    if (!settled) {
      settled = true;
      reject(err);
    }
  };

  const url = new URL(path, baseUrl);
  const payload = Buffer.from(JSON.stringify(body ?? {}));
  const req = http.request(
    url,
    {
      method: "POST",
      agent: false,
      headers: {
        "Content-Type": "application/json",
        "Content-Length": payload.length,
        Connection: "close",
      },
    },
    (res) => {
      res.resume();
      res.on("end", () => {
        if (res.statusCode && res.statusCode < 400) safeResolve();
        else safeReject(new Error(`${path} -> ${res.statusCode}`));
      });
    },
  );
  req.setTimeout(5000, () => {
    req.destroy(new Error(`httpPostJSON: timeout after 5s on ${path}`));
    safeReject(new Error(`httpPostJSON: timeout after 5s on ${path}`));
  });
  req.on("error", safeReject);
  req.end(payload);
  return promise;
}

export const tacticalControl = {
  reset: () => httpPostJSON(state.tacticalMockURL, "/__e2e__/reset", {}),
};
