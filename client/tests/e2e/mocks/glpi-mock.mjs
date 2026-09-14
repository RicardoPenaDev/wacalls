// Local, loopback-only HTTPS mock of the GLPI 11 High-Level REST API v2.3
// surface actually consumed by internal/glpi/client.go:
//   POST /api.php/token
//   GET  /api.php/v2.3/Assets/Computer
//   POST /api.php/v2.3/Assistance/Ticket
//   GET  /api.php/v2.3/Assistance/Ticket/{id}
//
// Plus a small control plane under /__e2e__/* used only by the Playwright
// suite to script scenarios (never called by the WACalls backend itself).
//
// Usage: node glpi-mock.mjs <port> <certPath> <keyPath> <expectedClientId> <expectedClientSecret> <expectedUsername> <expectedPassword>
//
// Listens on 127.0.0.1 only. Never reaches out anywhere; pure request/response
// state machine held in memory for the lifetime of the process.

import https from "node:https";
import fs from "node:fs";
import crypto from "node:crypto";

const [, , portArg, certPath, keyPath, expectedClientId, expectedClientSecret, expectedUsername, expectedPassword] =
  process.argv;
const port = Number(portArg);

const state = {
  token: null,
  tokenExpiresAt: 0,
  computers: [], // [{id, name, serial, entity}]
  tickets: new Map(), // id -> {href, externalId}
  nextTicketId: 1000,
  nextTicketMode: "success", // "success" | "429" | "5xx" | "malformed"
  nextRetryAfterSeconds: 2,
  lastAmbiguousTicketId: null,
  callCounts: { token: 0, computer: 0, ticketCreate: 0, ticketGet: 0 },
  // Lets the Playwright suite observe the "processing" state (persisted in
  // WACalls' DB via the atomic create+claim, D-018) before GLPI actually
  // answers: while held, the ticket-create HTTP response simply does not
  // complete until /__e2e__/release-held-ticket is called.
  holdNextTicketCreate: false,
  heldTicketResolvers: [],
};

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });
}

function sendJSON(res, status, obj) {
  const body = JSON.stringify(obj);
  res.writeHead(status, { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(body) });
  res.end(body);
}

function requireBearer(req, res) {
  const auth = req.headers["authorization"] || "";
  const ok = state.token && auth === `Bearer ${state.token}` && Date.now() < state.tokenExpiresAt;
  if (!ok) {
    sendJSON(res, 401, { error: "invalid_token" });
    return false;
  }
  return true;
}

async function handleToken(req, res) {
  state.callCounts.token++;
  const raw = await readBody(req);
  const form = new URLSearchParams(raw);
  const ok =
    form.get("grant_type") === "password" &&
    form.get("client_id") === expectedClientId &&
    form.get("client_secret") === expectedClientSecret &&
    form.get("username") === expectedUsername &&
    form.get("password") === expectedPassword &&
    form.get("scope") === "api";
  if (!ok) {
    sendJSON(res, 401, { error: "invalid_grant" });
    return;
  }
  state.token = crypto.randomBytes(16).toString("hex");
  const expiresIn = 3600;
  state.tokenExpiresAt = Date.now() + expiresIn * 1000;
  sendJSON(res, 200, { token_type: "Bearer", expires_in: expiresIn, access_token: state.token });
}

function handleComputerSearch(req, res, url) {
  state.callCounts.computer++;
  if (!requireBearer(req, res)) return;
  const filter = url.searchParams.get("filter") || "";
  const m = /name==(?:'((?:[^'\\]|\\.)*)')/.exec(filter);
  const wanted = m ? m[1].replace(/\\(.)/g, "$1") : null;
  const results = wanted ? state.computers.filter((c) => c.name.toUpperCase() === wanted.toUpperCase()) : state.computers;
  sendJSON(
    res,
    200,
    results.map((c) => ({
      id: c.id,
      name: c.name,
      serial: c.serial || "",
      entity: c.entity ? { id: c.entity.id, completename: c.entity.name } : null,
    })),
  );
}

async function handleTicketCreate(req, res) {
  state.callCounts.ticketCreate++;
  if (!requireBearer(req, res)) return;
  const raw = await readBody(req);
  let input;
  try {
    input = JSON.parse(raw);
  } catch {
    sendJSON(res, 400, { error: "invalid_json" });
    return;
  }

  if (state.holdNextTicketCreate) {
    await new Promise((resolve) => state.heldTicketResolvers.push(resolve));
  }

  const mode = state.nextTicketMode;
  if (mode === "429") {
    res.writeHead(429, { "Content-Type": "application/json", "Retry-After": String(state.nextRetryAfterSeconds) });
    res.end(JSON.stringify({ error: "rate_limited" }));
    return;
  }
  if (mode === "failed" || mode === "400") {
    sendJSON(res, 400, { error: "bad_request" });
    return;
  }
  if (mode === "malformed") {
    sendJSON(res, 201, { unexpected: "shape" });
    return;
  }

  // Both "success" and "5xx" actually persist the ticket: "5xx" simulates the
  // real-world ambiguous case where GLPI committed the ticket but the HTTP
  // response back to WACalls was lost (D-016). The Playwright suite retrieves
  // the silently-assigned id via GET /__e2e__/last-ambiguous-ticket to
  // reproduce what a human admin would find by checking the real GLPI UI.
  const id = String(state.nextTicketId++);
  const href = `https://127.0.0.1:${port}/api.php/v2.3/Assistance/Ticket/${id}`;
  state.tickets.set(id, { href, externalId: input.external_id || "" });

  if (mode === "5xx") {
    state.lastAmbiguousTicketId = id;
    sendJSON(res, 503, { error: "internal_error" });
    return;
  }

  sendJSON(res, 201, { id: Number(id), href });
}

function handleTicketGet(req, res, id) {
  state.callCounts.ticketGet++;
  if (!requireBearer(req, res)) return;
  const ticket = state.tickets.get(id);
  if (!ticket) {
    sendJSON(res, 404, { error: "not_found" });
    return;
  }
  sendJSON(res, 200, { id: Number(id), href: ticket.href, external_id: ticket.externalId });
}

async function handleControl(req, res, url) {
  if (req.method === "POST" && url.pathname === "/__e2e__/reset") {
    req.resume();
    state.computers = [];
    state.tickets.clear();
    state.nextTicketMode = "success";
    state.lastAmbiguousTicketId = null;
    state.callCounts = { token: 0, computer: 0, ticketCreate: 0, ticketGet: 0 };
    state.holdNextTicketCreate = false;
    for (const resolve of state.heldTicketResolvers.splice(0)) resolve();
    sendJSON(res, 200, { ok: true });
    return;
  }
  if (req.method === "POST" && url.pathname === "/__e2e__/computers") {
    const raw = await readBody(req);
    state.computers = JSON.parse(raw);
    sendJSON(res, 200, { ok: true });
    return;
  }
  if (req.method === "POST" && url.pathname === "/__e2e__/next-ticket-response") {
    const raw = await readBody(req);
    const body = JSON.parse(raw);
    state.nextTicketMode = body.mode;
    if (typeof body.retryAfterSeconds === "number") state.nextRetryAfterSeconds = body.retryAfterSeconds;
    sendJSON(res, 200, { ok: true });
    return;
  }
  if (req.method === "GET" && url.pathname === "/__e2e__/last-ambiguous-ticket") {
    req.resume();
    sendJSON(res, 200, { id: state.lastAmbiguousTicketId });
    return;
  }
  if (req.method === "GET" && url.pathname === "/__e2e__/call-counts") {
    req.resume();
    sendJSON(res, 200, state.callCounts);
    return;
  }
  if (req.method === "POST" && url.pathname === "/__e2e__/hold-next-ticket") {
    req.resume();
    state.holdNextTicketCreate = true;
    sendJSON(res, 200, { ok: true });
    return;
  }
  if (req.method === "POST" && url.pathname === "/__e2e__/release-held-ticket") {
    req.resume();
    state.holdNextTicketCreate = false;
    const resolvers = state.heldTicketResolvers.splice(0);
    for (const resolve of resolvers) resolve();
    sendJSON(res, 200, { ok: true, released: resolvers.length });
    return;
  }
  req.resume();
  sendJSON(res, 404, { error: "unknown_control_route" });
}

const server = https.createServer(
  { cert: fs.readFileSync(certPath), key: fs.readFileSync(keyPath) },
  async (req, res) => {
    const url = new URL(req.url, `https://127.0.0.1:${port}`);
    try {
      if (req.method === "GET" && url.pathname === "/health") {
        sendJSON(res, 200, { ok: true });
      } else if (url.pathname.startsWith("/__e2e__/")) {
        await handleControl(req, res, url);
      } else if (req.method === "POST" && url.pathname === "/api.php/token") {
        await handleToken(req, res);
      } else if (req.method === "GET" && url.pathname === "/api.php/v2.3/Assets/Computer") {
        handleComputerSearch(req, res, url);
      } else if (req.method === "POST" && url.pathname === "/api.php/v2.3/Assistance/Ticket") {
        await handleTicketCreate(req, res);
      } else if (req.method === "GET" && /^\/api\.php\/v2\.3\/Assistance\/Ticket\/[^/]+$/.test(url.pathname)) {
        const id = url.pathname.split("/").pop();
        handleTicketGet(req, res, id);
      } else {
        sendJSON(res, 404, { error: "not_found" });
      }
    } catch (err) {
      sendJSON(res, 500, { error: String(err && err.message ? err.message : err) });
    }
  },
);

server.listen(port, "127.0.0.1", () => {
  process.stdout.write(`glpi-mock listening on https://127.0.0.1:${port}\n`);
});

for (const sig of ["SIGTERM", "SIGINT"]) {
  process.on(sig, () => {
    server.close(() => process.exit(0));
  });
}
