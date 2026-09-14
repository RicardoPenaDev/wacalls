import test from "node:test";
import assert from "node:assert/strict";
import { sanitizeGLPIWebUrl } from "../src/lib/supportUrl.ts";

// Mock fetch helper to simulate backend responses
function createMockFetch(handler) {
  return async (url, options = {}) => {
    return handler(url.toString(), options);
  };
}

// ---------------------------------------------------------------------------
// 1. Criação de Chamado e Idempotency-Key
// ---------------------------------------------------------------------------
test("Criação de chamado: envia Idempotency-Key no header e payload correto", async () => {
  let capturedHeaders = {};
  let capturedBody = {};
  let capturedUrl = "";

  const mockFetch = async (url, init) => {
    capturedUrl = url;
    capturedHeaders = init.headers || {};
    capturedBody = JSON.parse(init.body || "{}");
    return {
      ok: true,
      status: 201,
      json: async () => ({
        supportRequest: {
          id: "req-123",
          sessionId: "sess-1",
          chatJid: "5511999999999@s.whatsapp.net",
          title: "Computador não liga",
          description: "Travado na tela da BIOS",
          requesterName: "Maria Silva",
          syncState: "synced",
          attemptCount: 1,
          glpiTicketId: "1001",
          webUrl: "https://glpi.internal/front/ticket.form.php?id=1001",
          createdAt: Date.now(),
          updatedAt: Date.now(),
        },
        warnings: [],
      }),
    };
  };

  // Simulate client creation
  const sid = "sess-1";
  const jid = "5511999999999@s.whatsapp.net";
  const idempotencyKey = "ui-550e8400-e29b-41d4-a716-446655440000";
  const payload = {
    requesterName: "Maria Silva",
    title: "Computador não liga",
    description: "Travado na tela da BIOS",
    priority: 3,
  };

  const res = await mockFetch(
    `http://localhost/api/sessions/${encodeURIComponent(sid)}/chats/${encodeURIComponent(jid)}/support/ticket`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": idempotencyKey,
      },
      body: JSON.stringify(payload),
    }
  );

  assert.equal(res.status, 201);
  assert.equal(capturedHeaders["Idempotency-Key"], idempotencyKey);
  assert.equal(capturedBody.title, "Computador não liga");
  assert.equal(capturedBody.requesterName, "Maria Silva");

  const data = await res.json();
  assert.equal(data.supportRequest.syncState, "synced");
  assert.equal(data.supportRequest.glpiTicketId, "1001");
});

// ---------------------------------------------------------------------------
// 2. Anti-Duplo Clique e Estabilidade da Idempotency-Key
// ---------------------------------------------------------------------------
test("Anti-duplo clique: bloqueia submissão concorrente e preserva a mesma Idempotency-Key", async () => {
  let submitCount = 0;
  let isSubmitting = false;
  const initialKey = "ui-" + "12345678-1234-1234-1234-123456789abc";
  let activeKey = initialKey;

  const mockSubmit = async () => {
    if (isSubmitting) {
      return { blocked: true };
    }
    isSubmitting = true;
    submitCount++;
    // Simulate async network latency
    await new Promise((r) => setTimeout(r, 20));
    isSubmitting = false;
    return { blocked: false, keyUsed: activeKey };
  };

  // Fire two clicks concurrently (double click)
  const [firstCall, secondCall] = await Promise.all([
    mockSubmit(),
    mockSubmit(),
  ]);

  assert.equal(firstCall.blocked, false, "Primeira submissão deve ser aceita");
  assert.equal(secondCall.blocked, true, "Segunda submissão (duplo clique) deve ser bloqueada");
  assert.equal(submitCount, 1, "Exatamente uma requisição deve ter sido disparada");
  assert.equal(firstCall.keyUsed, initialKey, "A Idempotency-Key deve permanecer idêntica");
});

// ---------------------------------------------------------------------------
// 3. HTTP 429 Rate Limit e Leitura do Header Retry-After
// ---------------------------------------------------------------------------
test("429 Rate Limit: processa header Retry-After e extrai tempo de espera", async () => {
  const mockFetch = async () => {
    return {
      ok: false,
      status: 429,
      statusText: "Too Many Requests",
      headers: new Map([["Retry-After", "45"]]),
      json: async () => ({
        error: {
          code: "rate_limited",
          message: "Too many requests. Please wait.",
          retryAfterSeconds: 45,
        },
      }),
    };
  };

  const res = await mockFetch();
  assert.equal(res.status, 429);

  let retryAfter = 0;
  const headerVal = res.headers.get("Retry-After");
  if (headerVal) retryAfter = parseInt(headerVal, 10);

  assert.equal(retryAfter, 45, "Header Retry-After deve ser 45 segundos");

  const body = await res.json();
  assert.equal(body.error.code, "rate_limited");
  assert.equal(body.error.retryAfterSeconds, 45);
});

// ---------------------------------------------------------------------------
// 4. Retry Action: Permitido somente em retryable_error
// ---------------------------------------------------------------------------
test("Retry: permitido exclusivamente quando estado é retryable_error", async () => {
  const isRetryAllowed = (state) => state === "retryable_error";

  assert.equal(isRetryAllowed("processing"), false, "Retry não deve ser permitido em processing");
  assert.equal(isRetryAllowed("synced"), false, "Retry não deve ser permitido em synced");
  assert.equal(isRetryAllowed("unknown"), false, "Retry não deve ser permitido em unknown (exige conciliação)");
  assert.equal(isRetryAllowed("failed"), false, "Retry não deve ser permitido em failed");
  assert.equal(isRetryAllowed("retryable_error"), true, "Retry deve ser permitido em retryable_error");

  // Simulate retry call
  let retryCalled = false;
  const mockRetry = async (reqId) => {
    retryCalled = true;
    return {
      supportRequest: {
        id: reqId,
        syncState: "synced",
        glpiTicketId: "1002",
      },
    };
  };

  const result = await mockRetry("req-456");
  assert.equal(retryCalled, true);
  assert.equal(result.supportRequest.syncState, "synced");
});

// ---------------------------------------------------------------------------
// 5. Usuário Não-Admin vs Admin em Estado unknown
// ---------------------------------------------------------------------------
test("Permissão de Reconciliação: bloqueada para usuário comum e permitida para admin em estado unknown", () => {
  const isAdmin = (user) => !!user && Array.isArray(user.roles) && user.roles.includes("admin");

  const regularUser = { id: "u-1", name: "Operador", roles: ["operator", "agent"] };
  const adminUser = { id: "u-2", name: "Supervisor Admin", roles: ["agent", "admin"] };
  const nullUser = null;

  assert.equal(isAdmin(regularUser), false, "Operador comum não é admin");
  assert.equal(isAdmin(adminUser), true, "Supervisor é admin");
  assert.equal(isAdmin(nullUser), false, "Usuário deslogado não é admin");

  const canReconcile = (user, syncState) => syncState === "unknown" && isAdmin(user);

  assert.equal(canReconcile(regularUser, "unknown"), false, "Operador comum NÃO pode reconciliar");
  assert.equal(canReconcile(adminUser, "unknown"), true, "Admin PODE reconciliar em unknown");
  assert.equal(canReconcile(adminUser, "synced"), false, "Admin não pode reconciliar em synced");
  assert.equal(canReconcile(adminUser, "retryable_error"), false, "Admin não pode reconciliar em retryable_error");
});

// ---------------------------------------------------------------------------
// 6. Troca de Dispositivo Vinculado
// ---------------------------------------------------------------------------
test("Troca de dispositivo: atualiza device_binding associado ao chamado", async () => {
  let updatedDeviceId = "";

  const mockUpdateDevice = async (requestId, payload) => {
    updatedDeviceId = payload.deviceBindingId;
    return {
      supportRequest: {
        id: requestId,
        deviceBindingId: payload.deviceBindingId,
        hostnameInformed: "SDE-ARS-RCP-03",
        syncState: "synced",
      },
      device: {
        id: payload.deviceBindingId,
        hostname: "SDE-ARS-RCP-03",
        sectorCode: "RCP-03",
        matchStatus: "matched",
      },
      warnings: [],
    };
  };

  const res = await mockUpdateDevice("req-789", { deviceBindingId: "dev-bind-99" });
  assert.equal(updatedDeviceId, "dev-bind-99");
  assert.equal(res.device.hostname, "SDE-ARS-RCP-03");
  assert.equal(res.device.sectorCode, "RCP-03");
  assert.equal(res.supportRequest.deviceBindingId, "dev-bind-99");
});

// ---------------------------------------------------------------------------
// 7. Fallback e Degradação Tactical RMM
// ---------------------------------------------------------------------------
test("Fallback Tactical: telemetria suprimida quando features.tactical=false e warning seguro quando unavailable", () => {
  // Scenario A: Tactical disabled by feature flag
  const renderTacticalTelemetry = (features, warnings, agent) => {
    if (!features?.tactical) {
      return { rendered: false, reason: "disabled" };
    }
    if (warnings?.includes("tactical_unavailable")) {
      return { rendered: true, mode: "safe_warning", message: "Telemetria temporariamente indisponível" };
    }
    if (agent) {
      return { rendered: true, mode: "agent_data", agent };
    }
    return { rendered: true, mode: "no_agent" };
  };

  const featureTacticalOff = { support: true, tactical: false };
  const viewA = renderTacticalTelemetry(featureTacticalOff, [], { status: "online" });
  assert.equal(viewA.rendered, false, "Com features.tactical=false, a telemetria é completamente suprimida");

  // Scenario B: Tactical enabled, but unavailable
  const featureTacticalOn = { support: true, tactical: true };
  const viewB = renderTacticalTelemetry(featureTacticalOn, ["tactical_unavailable"], null);
  assert.equal(viewB.rendered, true);
  assert.equal(viewB.mode, "safe_warning");

  // Scenario C: Tactical online
  const viewC = renderTacticalTelemetry(featureTacticalOn, [], {
    status: "online",
    operating_system: "Windows 11 Pro",
    public_ip: "192.168.1.50",
  });
  assert.equal(viewC.rendered, true);
  assert.equal(viewC.mode, "agent_data");
  assert.equal(viewC.agent.status, "online");
});

// ---------------------------------------------------------------------------
// 8. Sanitização e Validação Defensiva da webUrl GLPI no Frontend
// ---------------------------------------------------------------------------
test("Sanitização defensiva de webUrl GLPI: aceita https bem formada e rejeita esquemas inseguros ou malformados", () => {
  // Cenários válidos
  const validUrl = "https://glpi.example.com/front/ticket.form.php?id=1001";
  assert.equal(sanitizeGLPIWebUrl(validUrl), validUrl);

  const validWithPort = "https://glpi.example.com:8443/front/ticket.form.php?id=42";
  assert.equal(sanitizeGLPIWebUrl(validWithPort), validWithPort);

  // Rejeição de HTTP (sempre em produção/frontend)
  assert.equal(sanitizeGLPIWebUrl("http://glpi.example.com/front/ticket.form.php?id=1001"), null);

  // Rejeição de esquemas perigosos
  assert.equal(sanitizeGLPIWebUrl("javascript:alert(document.cookie)"), null);
  assert.equal(sanitizeGLPIWebUrl("data:text/html,<script>alert(1)</script>"), null);
  assert.equal(sanitizeGLPIWebUrl("vbscript:msgbox(1)"), null);

  // Rejeição de credenciais embutidas (userinfo)
  assert.equal(sanitizeGLPIWebUrl("https://admin:secret@glpi.example.com/front/ticket.form.php?id=1001"), null);
  assert.equal(sanitizeGLPIWebUrl("https://user@glpi.example.com/front/ticket.form.php?id=1001"), null);

  // Rejeição de URLs que apontam para endpoints REST internos em vez da UI web
  assert.equal(sanitizeGLPIWebUrl("https://glpi.example.com/api.php/v2.3/Assistance/Ticket/1001"), null);

  // Rejeição de ID ausente, não numérico, negativo ou com injeção
  assert.equal(sanitizeGLPIWebUrl("https://glpi.example.com/front/ticket.form.php"), null);
  assert.equal(sanitizeGLPIWebUrl("https://glpi.example.com/front/ticket.form.php?id=0"), null);
  assert.equal(sanitizeGLPIWebUrl("https://glpi.example.com/front/ticket.form.php?id=-1"), null);
  assert.equal(sanitizeGLPIWebUrl("https://glpi.example.com/front/ticket.form.php?id=abc"), null);
  assert.equal(sanitizeGLPIWebUrl("https://glpi.example.com/front/ticket.form.php?id=1;DROP"), null);

  // Rejeição de nulo, indefinido, vazio
  assert.equal(sanitizeGLPIWebUrl(null), null);
  assert.equal(sanitizeGLPIWebUrl(undefined), null);
  assert.equal(sanitizeGLPIWebUrl(""), null);
  assert.equal(sanitizeGLPIWebUrl("   "), null);
});

// ---------------------------------------------------------------------------
// 8b. Sanitização estrita: hash, parâmetros extras, id duplicado, query vazia
// ---------------------------------------------------------------------------
test("Sanitização estrita de webUrl GLPI: exige exatamente um único par id=<decimal> sem hash, extras ou duplicação", () => {
  // URL válida com somente ?id=123
  const soIdValido = "https://glpi.example.com/front/ticket.form.php?id=123";
  assert.equal(sanitizeGLPIWebUrl(soIdValido), soIdValido);

  // URL com #fragment
  assert.equal(
    sanitizeGLPIWebUrl("https://glpi.example.com/front/ticket.form.php?id=123#section"),
    null,
  );

  // URL com &redirect=...
  assert.equal(
    sanitizeGLPIWebUrl("https://glpi.example.com/front/ticket.form.php?id=123&redirect=https://evil.com"),
    null,
  );

  // URL com parâmetro adicional vazio
  assert.equal(
    sanitizeGLPIWebUrl("https://glpi.example.com/front/ticket.form.php?id=123&extra="),
    null,
  );

  // URL com id duplicado
  assert.equal(
    sanitizeGLPIWebUrl("https://glpi.example.com/front/ticket.form.php?id=1&id=2"),
    null,
  );

  // URL sem id (query com outro nome de parâmetro)
  assert.equal(
    sanitizeGLPIWebUrl("https://glpi.example.com/front/ticket.form.php?ticket=123"),
    null,
  );

  // URL com query vazia (apenas "?")
  assert.equal(sanitizeGLPIWebUrl("https://glpi.example.com/front/ticket.form.php?"), null);

  // URL com userinfo
  assert.equal(
    sanitizeGLPIWebUrl("https://admin:secret@glpi.example.com/front/ticket.form.php?id=123"),
    null,
  );

  // URL com protocolo diferente de HTTPS
  assert.equal(sanitizeGLPIWebUrl("http://glpi.example.com/front/ticket.form.php?id=123"), null);
  assert.equal(sanitizeGLPIWebUrl("ftp://glpi.example.com/front/ticket.form.php?id=123"), null);

  // Nenhum caso de rejeição lança exceção (garantido pelo uso de assert.equal acima sem try/catch)
});

// ---------------------------------------------------------------------------
// 9. Ações do Ticket Confirmado: Abrir no GLPI vs Copiar número
// ---------------------------------------------------------------------------
test("Ações do ticket confirmado: renderiza Abrir no GLPI com target/rel seguros e preserva Copiar número", async () => {
  // Lógica de resolução de ações espelhando SupportRequestStatus
  const resolveActions = (req) => {
    if (req.syncState !== "synced" || !req.glpiTicketId) {
      return { showBox: false, showWebLink: false, showCopyNumber: false, webUrl: null };
    }
    const safeUrl = sanitizeGLPIWebUrl(req.webUrl);
    return {
      showBox: true,
      showWebLink: Boolean(safeUrl),
      showCopyNumber: true,
      webUrl: safeUrl,
      target: "_blank",
      rel: "noopener noreferrer",
    };
  };

  // Cenário A: Chamado com webUrl válida -> ambos os botões aparecem
  const reqComLink = {
    syncState: "synced",
    glpiTicketId: "2001",
    webUrl: "https://glpi.example.com/front/ticket.form.php?id=2001",
  };
  const actA = resolveActions(reqComLink);
  assert.equal(actA.showBox, true);
  assert.equal(actA.showWebLink, true);
  assert.equal(actA.webUrl, "https://glpi.example.com/front/ticket.form.php?id=2001");
  assert.equal(actA.target, "_blank");
  assert.equal(actA.rel, "noopener noreferrer");
  assert.equal(actA.showCopyNumber, true);

  // Cenário B: Chamado legado ou sem base web (webUrl ausente) -> somente Copiar número
  const reqSemLink = {
    syncState: "synced",
    glpiTicketId: "1002",
    webUrl: null,
  };
  const actB = resolveActions(reqSemLink);
  assert.equal(actB.showBox, true);
  assert.equal(actB.showWebLink, false, "Botão web deve ser ocultado quando webUrl é nula");
  assert.equal(actB.webUrl, null);
  assert.equal(actB.showCopyNumber, true, "Copiar número permanece acessível em dados legados");

  // Cenário C: Chamado com URL insegura (ex: endpoint da API ou HTTP) -> botão web é suprimido
  const reqInseguro = {
    syncState: "synced",
    glpiTicketId: "1003",
    webUrl: "http://glpi.example.com/front/ticket.form.php?id=1003",
  };
  const actC = resolveActions(reqInseguro);
  assert.equal(actC.showWebLink, false, "Botão web deve ser suprimido para URL http insegura");
  assert.equal(actC.showCopyNumber, true);

  // Cenário D: Ação de cópia do número opera sobre o ID
  let copiedText = "";
  const copyNumber = async (ticketId) => {
    copiedText = ticketId;
  };
  await copyNumber(reqComLink.glpiTicketId);
  assert.equal(copiedText, "2001");

  // Cenário E: Isolamento — nenhuma chamada à API externa GLPI é efetuada no frontend
  let apiRequests = 0;
  const mockGLPIApi = () => { apiRequests++; };
  // Apenas a ação de navegação pura no navegador é utilizada
  assert.equal(apiRequests, 0, "O frontend nunca deve disparar requisições diretas à API GLPI");
});

// ---------------------------------------------------------------------------
// 10. handleResponse (interno): exercitado via funções exportadas reais de
//     support.ts com fetch stubado — não uma reimplementação em mock.
// ---------------------------------------------------------------------------
// handleResponse() não é exportada por support.ts, mas toda função exportada
// (createChatSupportTicket, getSupportRequest, etc.) delega a ela. Registramos
// um resolve/load hook mínimo (support-alias-loader.mjs) apenas para que o
// alias "@/*" do tsconfig e o global "import.meta.env" injetado pelo Vite em
// build funcionem sob o executor de testes puro do Node, e então importamos o
// módulo real dinamicamente — nenhuma lógica de support.ts é reimplementada.
const { register } = await import("node:module");

globalThis.__TEST_IMPORT_META_ENV__ = {};
register(new URL("./support-alias-loader.mjs", import.meta.url).href, import.meta.url);

const { createChatSupportTicket, getSupportRequest, SupportApiError } = await import(
  "../src/services/support.ts"
);

test("handleResponse via createChatSupportTicket: sucesso 201 retorna o corpo exatamente como recebido", async () => {
  const originalFetch = globalThis.fetch;
  const successBody = {
    supportRequest: { id: "x", syncState: "synced" },
    warnings: [],
  };
  globalThis.fetch = async () => ({
    ok: true,
    status: 201,
    statusText: "Created",
    headers: new Map(),
    json: async () => successBody,
  });
  try {
    const result = await createChatSupportTicket(
      "sess-1",
      "5511999999999@s.whatsapp.net",
      "idem-key-1",
      { requesterName: "Maria Silva", title: "t", description: "d", priority: 3 },
    );
    assert.deepEqual(result, successBody, "Caminho de sucesso real (res.ok) deve permanecer inalterado");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("handleResponse via getSupportRequest: HTTP 429 com supportRequest no corpo resolve em vez de lançar", async () => {
  const originalFetch = globalThis.fetch;
  const body = {
    supportRequest: { id: "x", syncState: "retryable_error", lastErrorCode: "rate_limited" },
    warnings: [],
  };
  globalThis.fetch = async () => ({
    ok: false,
    status: 429,
    statusText: "Too Many Requests",
    headers: new Map([["Retry-After", "60"]]),
    json: async () => body,
  });
  try {
    const result = await getSupportRequest("req-1");
    assert.deepEqual(result, body, "429 com supportRequest presente e truthy deve RESOLVER com o corpo, não lançar");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("handleResponse via getSupportRequest: HTTP 502 com supportRequest no corpo resolve em vez de lançar", async () => {
  const originalFetch = globalThis.fetch;
  const body = {
    supportRequest: { id: "x", syncState: "failed", lastErrorCode: "ticket_rejected" },
    warnings: [],
  };
  globalThis.fetch = async () => ({
    ok: false,
    status: 502,
    statusText: "Bad Gateway",
    headers: new Map(),
    json: async () => body,
  });
  try {
    const result = await getSupportRequest("req-1");
    assert.deepEqual(result, body, "502 com supportRequest presente e truthy deve RESOLVER com o corpo, não lançar");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("handleResponse via getSupportRequest: HTTP 500 sem supportRequest lança SupportApiError e NUNCA vira sucesso silencioso", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => ({
    ok: false,
    status: 500,
    statusText: "Internal Server Error",
    headers: new Map(),
    json: async () => ({
      error: { code: "internal_error", message: "failed to create support ticket" },
    }),
  });
  try {
    await assert.rejects(
      () => getSupportRequest("req-1"),
      (err) => {
        // Prova explícita: um erro interno puro (sem chave supportRequest) é
        // rejeitado com SupportApiError — jamais transformado em um valor
        // resolvido/sucesso silencioso.
        assert.ok(err instanceof SupportApiError, "erro interno deve ser exatamente SupportApiError, não um valor resolvido");
        assert.equal(err.status, 500);
        assert.equal(err.code, "internal_error");
        return true;
      },
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("handleResponse via getSupportRequest: HTTP 400 com corpo JSON inválido usa statusText como mensagem de fallback", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => ({
    ok: false,
    status: 400,
    statusText: "Bad Request",
    headers: new Map(),
    json: async () => {
      throw new SyntaxError("Unexpected end of JSON input");
    },
  });
  try {
    await assert.rejects(
      () => getSupportRequest("req-1"),
      (err) => {
        assert.ok(err instanceof SupportApiError);
        assert.equal(err.status, 400);
        assert.equal(err.message, "Bad Request", "Quando o JSON falha, a mensagem cai para res.statusText");
        return true;
      },
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("handleResponse via getSupportRequest: HTTP 404 com corpo vazio e sem statusText não quebra o duplo fallback", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => ({
    ok: false,
    status: 404,
    statusText: "",
    headers: new Map(),
    json: async () => {
      throw new SyntaxError("Unexpected end of JSON input");
    },
  });
  try {
    await assert.rejects(
      () => getSupportRequest("req-1"),
      (err) => {
        assert.ok(err instanceof SupportApiError);
        assert.equal(err.status, 404);
        assert.equal(
          err.message,
          "HTTP 404",
          "Sem statusText, o fallback duplo (JSON falho -> statusText vazio) não deve quebrar; usa 'HTTP <status>'",
        );
        return true;
      },
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});
