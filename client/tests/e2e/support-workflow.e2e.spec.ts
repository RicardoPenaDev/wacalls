import { test, expect, loginAs, state, glpiControl } from "./fixtures";

test.describe("Fluxo de Suporte Técnico E2E (GLPI + Tactical)", () => {
  test.beforeEach(async () => {
    await glpiControl.reset();
  });

  test("caminho feliz: anti-duplo clique, estado processing observável, sincronização com GLPI e ações do ticket confirmado (processing -> synced)", async ({
    page,
  }) => {
    await glpiControl.setNextTicketResponse("success");
    // Holds the GLPI mock's response so the backend's ticket-create call
    // hangs after it has already persisted+claimed the row as "processing"
    // (D-018) — this lets the test observe "processing" as a real,
    // separately-fetched state, not just an inferred transition.
    await glpiControl.holdNextTicket();

    const chatJid = state.chatJids[0];
    await loginAs(page, state.admin);
    await page.goto(`/chats?sid=${state.sessionId}&jid=${encodeURIComponent(chatJid)}`);

    // Abre o painel de suporte
    const supportButton = page.getByRole("button", { name: /suporte/i });
    await expect(supportButton).toBeVisible({ timeout: 10_000 });
    await supportButton.click();

    // Formulário de abertura
    const requesterInput = page.locator("#support-requester");
    await expect(requesterInput).toBeVisible({ timeout: 5_000 });
    await requesterInput.fill("Ricardo Silva");

    const titleInput = page.locator("#support-title");
    await titleInput.fill("Computador travando ao iniciar");

    const descInput = page.locator("#support-desc");
    await descInput.fill("O equipamento apresenta lentidão excessiva e travamentos intermitentes após login.");

    // Intercepta e conta requisições POST para validar garantia anti-duplo clique
    let postCount = 0;
    const idempotencyKeys: string[] = [];
    await page.route("**/api/sessions/*/chats/*/support/ticket", (route) => {
      if (route.request().method() === "POST") {
        postCount++;
        const idemp = route.request().headers()["idempotency-key"] || "";
        idempotencyKeys.push(idemp);
      }
      route.continue();
    });

    const submitButton = page.getByRole("button", { name: /criar chamado glpi/i });
    await expect(submitButton).toBeEnabled();

    // Dispara múltiplos cliques rápidos no botão de submissão
    await Promise.all([submitButton.click(), submitButton.click().catch(() => {}), submitButton.click().catch(() => {})]);

    // Valida que exatamente 1 requisição POST foi enviada com a Idempotency-Key
    await expect.poll(() => postCount, { timeout: 10_000 }).toBe(1);
    expect(idempotencyKeys.length).toBe(1);
    expect(idempotencyKeys[0]).toMatch(/^ui-/);

    // A requisição POST acima ainda está pendurada no mock; o backend já
    // persistiu e reivindicou a linha como "processing" antes de chamar o
    // GLPI (D-018). Um recarregamento MANUAL e SEPARADO (GET, não o POST
    // pendurado) deve observar esse estado real.
    const refreshButton = page.getByTitle(/recarregar dados de suporte/i);
    const processingBadge = page.getByText(/Sincronizando/i);
    await expect
      .poll(
        async () => {
          await refreshButton.click();
          return processingBadge.isVisible().catch(() => false);
        },
        { timeout: 10_000, intervals: [300] },
      )
      .toBe(true);

    // Libera o mock: o POST original agora completa com sucesso.
    await glpiControl.releaseHeldTicket();

    // Valida transição para estado "Sincronizado"
    const syncedBadge = page.getByText(/Sincronizado/i);
    await expect(syncedBadge).toBeVisible({ timeout: 10_000 });

    // Valida número do ticket renderizado
    await expect(page.getByText(/#1000/)).toBeVisible();

    // Valida timestamp de criação: período atual e sem 1970
    await expect(page.getByText(/Criado em:/)).toBeVisible();
    const createdText = await page.getByText(/Criado em:/).textContent();
    expect(createdText).toBeTruthy();
    expect(createdText).not.toContain("1970");
    const currentYear = new Date().getFullYear().toString();
    expect(createdText).toContain(currentYear);

    // Valida link web seguro para o GLPI
    const webLink = page.getByRole("link", { name: /abrir no glpi/i });
    await expect(webLink).toBeVisible();
    await expect(webLink).toHaveAttribute("target", "_blank");
    await expect(webLink).toHaveAttribute("rel", "noopener noreferrer");
    const href = await webLink.getAttribute("href");
    expect(href).toMatch(/^https:\/\/127\.0\.0\.1:\d+\/front\/ticket\.form\.php\?id=1000$/);
    const urlBeforeCopy = page.url();

    // Valida "Copiar número": copia o id do ticket para a área de
    // transferência real e NÃO navega de fato para o GLPI.
    const copyButton = page.getByRole("button", { name: /copiar número/i });
    await copyButton.click();
    await expect(page.getByText(/copiado!/i)).toBeVisible({ timeout: 2_000 });
    const clipboardText = await page.evaluate(() => navigator.clipboard.readText());
    expect(clipboardText).toBe("1000");
    expect(page.url()).toBe(urlBeforeCopy);
  });

  test("falha recuperável (429 & Retry-After) e retry com sucesso", async ({ page }) => {
    // O backend sempre anuncia Retry-After: 60 para rate_limited (contrato
    // fixo em supportapi.go, independente do valor que o GLPI real enviar);
    // este teste espera o prazo real, então precisa de mais tempo que o
    // timeout padrão do arquivo de configuração (120s para tolerar lentidão em CI).
    test.setTimeout(120_000);
    await glpiControl.setNextTicketResponse("429");

    const chatJid = state.chatJids[1];
    await loginAs(page, state.admin);
    await page.goto(`/chats?sid=${state.sessionId}&jid=${encodeURIComponent(chatJid)}`);

    const supportButton = page.getByRole("button", { name: /suporte/i });
    await expect(supportButton).toBeVisible({ timeout: 10_000 });
    await supportButton.click();

    await page.locator("#support-requester").fill("Ricardo Silva");
    await page.locator("#support-title").fill("Impressora sem comunicação");
    await page.locator("#support-desc").fill("Impressora de rede parou de responder às solicitações de impressão.");

    let ticketPostCount = 0;
    await page.route("**/api/sessions/*/chats/*/support/ticket", (route) => {
      if (route.request().method() === "POST") ticketPostCount++;
      route.continue();
    });

    const submitButton = page.getByRole("button", { name: /criar chamado glpi/i });
    await submitButton.click();

    // Valida exibição do badge de Falha Recuperável
    const retryableBadge = page.getByText(/Falha Recuperável/i);
    await expect(retryableBadge).toBeVisible({ timeout: 10_000 });

    // Configura GLPI mock para responder com sucesso no retry
    await glpiControl.setNextTicketResponse("success");

    // O botão de retry deve ficar bloqueado ANTES do prazo de Retry-After
    // (countdown) e nenhuma requisição de retry deve ser disparada antes
    // disso. O rótulo do botão muda conforme o estado (countdown vs.
    // pronto), então o locator casa com ambos.
    const retryButton = page.getByRole("button", { name: /repetir criação|aguarde \d+s/i });
    await expect(retryButton).toBeVisible({ timeout: 10_000 });
    await expect(retryButton).toBeDisabled();

    let retryPostCount = 0;
    await page.route("**/api/support/requests/*/retry", (route) => {
      retryPostCount++;
      route.continue();
    });
    expect(retryPostCount).toBe(0);

    // Aguarda o botão de retry ficar disponível após o prazo real de
    // Retry-After (60s, contrato fixo do backend — ver comentário acima).
    await expect(retryButton).toBeEnabled({ timeout: 65_000 });
    await expect(retryButton).toHaveText(/repetir criação \(retry\)/i);
    expect(retryPostCount).toBe(0); // ficar habilitado != disparar sozinho

    await retryButton.click();

    // Valida transição com sucesso para Sincronizado
    const syncedBadge = page.getByText(/Sincronizado/i);
    await expect(syncedBadge).toBeVisible({ timeout: 10_000 });
    expect(retryPostCount).toBe(1);
    expect(ticketPostCount).toBe(1);
  });

  test("segregação de permissões e conciliação administrativa (unknown -> synced)", async ({ page, context }) => {
    // Simula falha 5xx/ambígua do GLPI
    await glpiControl.setNextTicketResponse("5xx");

    const chatJid = state.chatJids[2];

    // 1. Operador comum submete e entra em estado 'unknown'
    await loginAs(page, state.operator);
    await page.goto(`/chats?sid=${state.sessionId}&jid=${encodeURIComponent(chatJid)}`);

    const supportBtn = page.getByRole("button", { name: /suporte/i });
    await expect(supportBtn).toBeVisible({ timeout: 10_000 });
    await supportBtn.click();

    await page.locator("#support-requester").fill("Atendente Operador");
    await page.locator("#support-title").fill("Erro de sincronização 5xx");
    await page.locator("#support-desc").fill("Chamado disparado durante instabilidade no GLPI para teste de conciliação.");

    await page.getByRole("button", { name: /criar chamado glpi/i }).click();

    // Valida que o chamado entrou em Confirmação Pendente (unknown)
    const unknownBadge = page.getByText(/Confirmação Pendente/i);
    await expect(unknownBadge).toBeVisible({ timeout: 10_000 });

    // Operador comum NÃO deve visualizar o botão de conciliação administrativa
    const reconcileBtn = page.getByRole("button", { name: /conciliar chamado \(admin\)/i });
    await expect(reconcileBtn).not.toBeVisible();
    await expect(page.getByText(/solicite a conciliação manual a um supervisor/i)).toBeVisible();

    // 2. Administrador acessa o mesmo atendimento e realiza a conciliação
    await context.clearCookies();
    await loginAs(page, state.admin);
    await page.goto(`/chats?sid=${state.sessionId}&jid=${encodeURIComponent(chatJid)}`);

    const adminSupportBtn = page.getByRole("button", { name: /suporte/i });
    await expect(adminSupportBtn).toBeVisible({ timeout: 10_000 });
    await adminSupportBtn.click();

    // Administrador DEVE visualizar o botão de conciliação administrativa
    const adminReconcileBtn = page.getByRole("button", { name: /conciliar chamado \(admin\)/i });
    await expect(adminReconcileBtn).toBeVisible({ timeout: 10_000 });
    await adminReconcileBtn.click();

    // Modal de conciliação aberto
    const modalTitle = page.getByText("Conciliação Administrativa (GLPI)");
    await expect(modalTitle).toBeVisible();

    // Obtém o ID que foi gravado pelo mock no modo 5xx
    const { id: ambiguousId } = await glpiControl.lastAmbiguousTicket();
    expect(ambiguousId).toBeTruthy();

    const idInput = page.locator("#reconcile-ticket-id");
    await expect(idInput).toBeVisible();
    await idInput.fill(String(ambiguousId));

    const confirmBtn = page.getByRole("button", { name: /executar conciliação/i });
    await confirmBtn.click();

    // Valida que o estado foi conciliado e transicionou para Sincronizado
    const syncedBadge = page.getByText(/Sincronizado/i);
    await expect(syncedBadge).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText(new RegExp(`#${ambiguousId}`))).toBeVisible();
  });

  test("falha permanente (failed) e reabertura de novo formulário", async ({ page }) => {
    // Simula erro 400 (rejeitado definitivamente)
    await glpiControl.setNextTicketResponse("400");

    const chatJid = state.chatJids[3];
    await loginAs(page, state.admin);
    await page.goto(`/chats?sid=${state.sessionId}&jid=${encodeURIComponent(chatJid)}`);

    const supportBtn = page.getByRole("button", { name: /suporte/i });
    await expect(supportBtn).toBeVisible({ timeout: 10_000 });
    await supportBtn.click();

    await page.locator("#support-requester").fill("Ricardo Silva");
    await page.locator("#support-title").fill("Chamado que falhará 400");
    await page.locator("#support-desc").fill("Descrição para teste de falha permanente.");

    await page.getByRole("button", { name: /criar chamado glpi/i }).click();

    // Valida exibição do badge de Falha Permanente
    const failedBadge = page.getByText(/Falha Permanente/i);
    await expect(failedBadge).toBeVisible({ timeout: 10_000 });

    // Valida que ação de criar novo chamado fica disponível
    const reopenBtn = page.getByRole("button", { name: /criar novo chamado/i });
    await expect(reopenBtn).toBeVisible();
    await reopenBtn.click();

    // Valida que o formulário é reexibido limpo
    await expect(page.locator("#support-title")).toBeVisible();
    expect(await page.locator("#support-title").inputValue()).toBe("");
  });

  test("cancelamento de polling ao fechar o painel de suporte", async ({ page }) => {
    const chatJid = state.chatJids[4];
    await loginAs(page, state.admin);
    await page.goto(`/chats?sid=${state.sessionId}&jid=${encodeURIComponent(chatJid)}`);

    const supportBtn = page.getByRole("button", { name: /suporte/i });
    await expect(supportBtn).toBeVisible({ timeout: 10_000 });
    await supportBtn.click();

    // Aguarda o formulário abrir
    await expect(page.locator("#support-requester")).toBeVisible({ timeout: 10_000 });

    // Fecha o painel clicando novamente no botão de suporte
    await supportBtn.click();

    // Monitora tráfego de rede durante 3 segundos confirmando ausência de requisições contínuas de polling
    let pollRequests = 0;
    page.on("request", (req) => {
      if (req.url().includes("/support") && req.method() === "GET") {
        pollRequests++;
      }
    });

    await page.waitForTimeout(3000);
    // Não deve haver chamadas contínuas de polling com o painel fechado
    expect(pollRequests).toBeLessThanOrEqual(1);
  });

  test("painel em conversa com equipamento vinculado: hostname e status de vínculo visíveis", async ({ page }) => {
    await glpiControl.setNextTicketResponse("success");

    const chatJid = state.chatJids[5];
    await loginAs(page, state.admin);
    await page.goto(`/chats?sid=${state.sessionId}&jid=${encodeURIComponent(chatJid)}`);

    const supportButton = page.getByRole("button", { name: /suporte/i });
    await expect(supportButton).toBeVisible({ timeout: 10_000 });
    await supportButton.click();
    await expect(page.locator("#support-requester")).toBeVisible({ timeout: 5_000 });

    // Nenhum equipamento vinculado ainda — estado vazio exibido.
    await expect(page.getByText(/nenhum computador vinculado a este contato/i)).toBeVisible();

    await page.getByRole("button", { name: /localizar e vincular computador/i }).click();
    await page.getByPlaceholder(/buscar por hostname/i).fill(state.fixtureDevice.hostname);

    const resultItem = page.getByRole("button", { name: new RegExp(state.fixtureDevice.hostname, "i") });
    await expect(resultItem).toBeVisible({ timeout: 5_000 });
    await resultItem.click();
    await page.getByRole("button", { name: /confirmar vinculação/i }).click();

    // Hostname e status de vínculo visíveis no formulário ANTES da
    // submissão. getByTitle mira especificamente o span do DeviceSummary
    // (title={hostname}), evitando colisão com o item de resultado da
    // busca do DevicePickerModal, que também contém o texto do hostname.
    await expect(page.getByTitle(state.fixtureDevice.hostname, { exact: true })).toBeVisible();
    await expect(page.getByText("Vinculado", { exact: true })).toBeVisible();

    await page.locator("#support-requester").fill("Ricardo Silva");
    await page.locator("#support-title").fill("Solicitação com equipamento vinculado");
    await page.locator("#support-desc").fill("Chamado de teste E2E validando exibição do equipamento vinculado no painel.");
    await page.getByRole("button", { name: /criar chamado glpi/i }).click();

    const syncedBadge = page.getByText(/Sincronizado/i);
    await expect(syncedBadge).toBeVisible({ timeout: 10_000 });

    // Hostname continua visível depois de o chamado ser criado.
    await expect(page.getByTitle(state.fixtureDevice.hostname, { exact: true })).toBeVisible();
  });

  test("polling encerrado ao trocar de conversa", async ({ page }) => {
    await glpiControl.setNextTicketResponse("success");
    await glpiControl.holdNextTicket();

    const chatJid = state.chatJids[6];
    const otherChatJid = state.chatJids[0];
    await loginAs(page, state.admin);
    await page.goto(`/chats?sid=${state.sessionId}&jid=${encodeURIComponent(chatJid)}`);

    const supportBtn = page.getByRole("button", { name: /suporte/i });
    await expect(supportBtn).toBeVisible({ timeout: 10_000 });
    await supportBtn.click();

    await page.locator("#support-requester").fill("Ricardo Silva");
    await page.locator("#support-title").fill("Chamado para teste de troca de conversa");
    await page
      .locator("#support-desc")
      .fill("Verifica se o polling para quando o operador troca de conversa com o chamado em processing.");
    await page.getByRole("button", { name: /criar chamado glpi/i }).click();

    // A requisição POST está pendurada no mock; recarrega manualmente para
    // observar o estado "processing" real e persistido (mesmo mecanismo do
    // teste do caminho feliz), antes de trocar de conversa.
    const refreshButton = page.getByTitle(/recarregar dados de suporte/i);
    const processingBadge = page.getByText(/Sincronizando/i);
    await expect
      .poll(
        async () => {
          await refreshButton.click();
          return processingBadge.isVisible().catch(() => false);
        },
        { timeout: 10_000, intervals: [300] },
      )
      .toBe(true);

    let pollCount = 0;
    page.on("request", (req) => {
      if (/\/api\/support\/requests\/[^/]+$/.test(new URL(req.url()).pathname) && req.method() === "GET") {
        pollCount++;
      }
    });

    // Troca para outra conversa com o painel ainda em processing.
    await page.goto(`/chats?sid=${state.sessionId}&jid=${encodeURIComponent(otherChatJid)}`);
    await expect(page.locator("#support-requester")).toBeHidden({ timeout: 5_000 });

    const countRightAfterSwitch = pollCount;
    // Aguarda mais que o intervalo de polling do painel (3s) para confirmar
    // que nenhuma nova consulta referente ao request/conversa anterior ocorre.
    await page.waitForTimeout(4_000);
    expect(pollCount).toBe(countRightAfterSwitch);

    await glpiControl.releaseHeldTicket();
  });

  test("rascunho do composer e conversa selecionada são preservados ao abrir e fechar o painel de suporte", async ({ page }) => {
    const chatJid = state.chatJids[7];
    await loginAs(page, state.admin);
    await page.goto(`/chats?sid=${state.sessionId}&jid=${encodeURIComponent(chatJid)}`);

    const composer = page.getByPlaceholder(/digite uma mensagem/i);
    await expect(composer).toBeVisible({ timeout: 10_000 });
    const draftText = "Rascunho de teste E2E - não deve ser perdido";
    await composer.fill(draftText);

    const urlBefore = page.url();

    const supportButton = page.getByRole("button", { name: /suporte/i });
    await supportButton.click();
    await expect(page.locator("#support-requester")).toBeVisible({ timeout: 10_000 });

    // Fecha o painel clicando determinísticamente no botão "Fechar"
    // restrito ao diálogo do painel aberto.
    const panel = page.getByRole("dialog", { name: /suporte glpi/i });
    const closeBtn = panel.getByRole("button", { name: "Fechar", exact: true });
    await expect(closeBtn).toBeVisible({ timeout: 5_000 });
    await closeBtn.click();
    await expect(page.locator("#support-requester")).toBeHidden({ timeout: 5_000 });

    // O rascunho do composer permanece intacto e a conversa/URL não mudou.
    await expect(composer).toHaveValue(draftText, { timeout: 5_000 });
    expect(page.url()).toBe(urlBefore);
  });
});
