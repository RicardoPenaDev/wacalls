// Self-test for the network guard itself (client/tests/e2e/fixtures.ts).
//
// Uses a clearly-fake, unregistered hostname ("external.invalid.test") as
// the probe target. Playwright's context.route()/routeWebSocket() intercept
// at the render-process network layer BEFORE any real DNS resolution or
// socket connect happens, so this never depends on internet access or on
// the probe host actually existing — the guard either intercepts the
// attempt (pass) or the request would otherwise escape to a real socket
// (which the CI sandbox has no route to reach anyway).
//
// Both tests request the `networkOffenders` fixture directly (not just the
// auto-injected `networkGuard`) so they can assert on the recorded offender
// and then clear it — otherwise the auto `networkGuard` fixture's own
// end-of-test `expect(offenders).toEqual([])` would fail every test that
// deliberately triggers one.
import { test, expect } from "./fixtures";

test.describe("Network guard (self-test)", () => {
  test("bloqueia requisição HTTP/HTTPS para host externo e registra o offender", async ({ page, networkOffenders }) => {
    await page.goto("/login");
    expect(networkOffenders).toEqual([]);

    const fetchOutcome = await page.evaluate(() =>
      fetch("https://external.invalid.test/probe")
        .then(() => "resolved")
        .catch((err) => `rejected:${String(err)}`),
    );

    expect(fetchOutcome).toMatch(/^rejected:/);
    expect(networkOffenders).toEqual(["https://external.invalid.test/probe"]);

    // Consume the intentional offender so the auto networkGuard fixture's
    // own post-test assertion (which runs right after this test function
    // returns) still sees an empty, clean list.
    networkOffenders.length = 0;
  });

  test("bloqueia WebSocket para host externo e registra o offender, sem tocar em conexão real", async ({ page, networkOffenders }) => {
    await page.goto("/login");
    expect(networkOffenders).toEqual([]);

    const wsOutcome = await page.evaluate(
      () =>
        new Promise<string>((resolve) => {
          const ws = new WebSocket("wss://external.invalid.test/socket");
          const timeout = setTimeout(() => resolve("timeout"), 5000);
          ws.addEventListener("close", (ev) => {
            clearTimeout(timeout);
            resolve(`closed:${ev.code}`);
          });
          ws.addEventListener("error", () => {
            clearTimeout(timeout);
            resolve("error");
          });
        }),
    );

    expect(wsOutcome).not.toBe("timeout");
    expect(networkOffenders).toEqual(["wss://external.invalid.test/socket"]);

    networkOffenders.length = 0;
  });

  test("permite tráfego loopback normalmente (sem offenders)", async ({ page, networkOffenders }) => {
    await page.goto("/login");
    await page.locator("#email").waitFor({ state: "visible" });
    expect(networkOffenders).toEqual([]);
  });
});
