// Runs against a SEPARATE, genuinely different backend boot (see
// client/tests/e2e/run.mjs's "tactical-disabled" phase and
// client/tests/e2e/lib/orchestrator.mjs's `withTactical` option): no
// Tactical mock process exists at all in this phase, and the backend never
// receives WACALLS_TACTICAL_BASE_URL/WACALLS_TACTICAL_API_KEY. This is not a
// frontend flag layered on top of the normal environment — the backend's
// own `isTacticalEnabled()` (cmd/server/supportapi.go) is false because
// `s.tacticalClient` is genuinely nil, and `/api/options` reports
// `features.tactical: false` from that same real state
// (cmd/server/settingsapi.go).
import { test, expect, loginAs, state, glpiControl } from "./fixtures";

test.describe("Suporte com Tactical desabilitado (boot real sem WACALLS_TACTICAL_*)", () => {
  test.beforeEach(async () => {
    await glpiControl.reset();
  });

  test("support=true, tactical=false: telemetria ausente e criação de chamado GLPI continua funcionando", async ({ page }) => {
    expect(state.tacticalEnabled).toBe(false);
    expect(state.tacticalMockURL).toBeNull();

    await glpiControl.setNextTicketResponse("success");

    await loginAs(page, state.admin);
    await page.goto(`/chats?sid=${state.sessionId}&jid=${encodeURIComponent(state.chatJid)}`);

    const supportButton = page.getByRole("button", { name: /suporte/i });
    await expect(supportButton).toBeVisible({ timeout: 10_000 });
    await supportButton.click();
    await expect(page.locator("#support-requester")).toBeVisible({ timeout: 5_000 });

    // Nenhuma telemetria Tactical é renderizada quando o backend foi
    // iniciado sem WACALLS_TACTICAL_*. "Nenhum agente Tactical vinculado..."
    // e o aviso de indisponibilidade só aparecem quando features.tactical
    // é verdadeiro (TacticalStatus retorna null caso contrário) — a
    // ausência de ambos prova que o componente não renderizou.
    await expect(page.getByText(/nenhum agente tactical vinculado/i)).toHaveCount(0);
    await expect(page.getByText(/telemetria tactical rmm indisponível/i)).toHaveCount(0);

    await page.locator("#support-requester").fill("Ricardo Silva");
    await page.locator("#support-title").fill("Chamado sem Tactical configurado");
    await page
      .locator("#support-desc")
      .fill("Valida que a criação de chamado GLPI continua funcionando com Tactical desabilitado no backend.");
    await page.getByRole("button", { name: /criar chamado glpi/i }).click();

    const syncedBadge = page.getByText(/Sincronizado/i);
    await expect(syncedBadge).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText(/#1000/)).toBeVisible();
  });
});
