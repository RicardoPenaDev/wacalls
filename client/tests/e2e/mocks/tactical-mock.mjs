// Local, loopback-only HTTP mock of the Tactical RMM surface actually
// consumed by internal/tactical/client.go:
//   GET /agents/
//   GET /agents/{id}/
// Auth via header X-API-KEY. Plain HTTP is acceptable here: WACALLS_TACTICAL_*
// has no HTTPS-only hardening (unlike WACALLS_GLPI_WEB_BASE_URL), and the
// Tactical client itself never touches the browser.
//
// Usage: node tactical-mock.mjs <port> <expectedApiKey>
//
// Control plane under /__e2e__/* is for the Playwright suite only.

import http from "node:http";

const [, , portArg, expectedApiKey] = process.argv;
const port = Number(portArg);

const state = {
  agents: new Map(), // id -> agent detail object (Tactical wire shape)
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

function requireApiKey(req, res) {
  if (req.headers["x-api-key"] !== expectedApiKey) {
    sendJSON(res, 401, { detail: "invalid api key" });
    return false;
  }
  return true;
}

async function handleControl(req, res, url) {
  if (req.method === "POST" && url.pathname === "/__e2e__/reset") {
    req.resume();
    state.agents.clear();
    sendJSON(res, 200, { ok: true });
    return;
  }
  if (req.method === "POST" && url.pathname === "/__e2e__/agents") {
    const raw = await readBody(req);
    const agents = JSON.parse(raw);
    state.agents.clear();
    for (const a of agents) state.agents.set(String(a.agent_id), a);
    sendJSON(res, 200, { ok: true });
    return;
  }
  req.resume();
  sendJSON(res, 404, { error: "unknown_control_route" });
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, `http://127.0.0.1:${port}`);
  try {
    if (req.method === "GET" && url.pathname === "/health") {
      sendJSON(res, 200, { ok: true });
      return;
    }
    if (url.pathname.startsWith("/__e2e__/")) {
      await handleControl(req, res, url);
      return;
    }
    if (req.method === "GET" && url.pathname === "/agents/") {
      if (!requireApiKey(req, res)) return;
      sendJSON(res, 200, [...state.agents.values()]);
      return;
    }
    const agentMatch = /^\/agents\/([^/]+)\/$/.exec(url.pathname);
    if (req.method === "GET" && agentMatch) {
      if (!requireApiKey(req, res)) return;
      const agent = state.agents.get(agentMatch[1]);
      if (!agent) {
        sendJSON(res, 404, { detail: "not found" });
        return;
      }
      sendJSON(res, 200, agent);
      return;
    }
    sendJSON(res, 404, { error: "not_found" });
  } catch (err) {
    sendJSON(res, 500, { error: String(err && err.message ? err.message : err) });
  }
});

server.listen(port, "127.0.0.1", () => {
  process.stdout.write(`tactical-mock listening on http://127.0.0.1:${port}\n`);
});

for (const sig of ["SIGTERM", "SIGINT"]) {
  process.on(sig, () => {
    server.close(() => process.exit(0));
  });
}
