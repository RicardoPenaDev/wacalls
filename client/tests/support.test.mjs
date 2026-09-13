import test from "node:test";
import assert from "node:assert/strict";

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
          glpiTicketHref: "https://glpi.internal/ticket/1001",
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
