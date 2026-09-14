// Ephemeral loopback port allocation for the E2E orchestrator.
//
// We never hardcode ports. `getFreePort` binds a throwaway TCP listener on
// 127.0.0.1 to let the OS pick a free port, reads it back, then releases it
// immediately so the real process (Go backend, Node mock, Vite preview) can
// bind it. This has an inherent, accepted race: another process could grab
// the same port between release and re-bind. `startOnFreePort` covers that
// race by retrying with a freshly allocated port whenever the caller reports
// a bind failure (e.g. EADDRINUSE, or the readiness probe never succeeds).

import net from "node:net";

/** Binds to port 0 on 127.0.0.1, returns the OS-assigned port, then releases it. */
export function getFreePort() {
  const { promise, resolve, reject } = Promise.withResolvers();
  const srv = net.createServer();
  srv.unref();
  srv.on("error", reject);
  srv.listen(0, "127.0.0.1", () => {
    const { port } = srv.address();
    srv.close((err) => {
      if (err) reject(err);
      else resolve(port);
    });
  });
  return promise;
}

/**
 * Allocates a free port and calls `attempt(port)`. If `attempt` throws (or
 * its returned promise rejects) with `err.retryable === true`, a new port is
 * allocated and the attempt is retried, up to `maxAttempts` times total.
 * Returns whatever `attempt` resolves to (conventionally including the port
 * used, so the caller doesn't have to thread it separately).
 */
export async function startOnFreePort(attempt, { maxAttempts = 5 } = {}) {
  let lastErr;
  for (let i = 0; i < maxAttempts; i++) {
    const port = await getFreePort();
    try {
      return await attempt(port);
    } catch (err) {
      lastErr = err;
      if (!err || err.retryable !== true) throw err;
    }
  }
  throw lastErr ?? new Error("startOnFreePort: exhausted attempts with no error captured");
}
