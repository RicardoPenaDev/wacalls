# T-002 — Planejamento técnico do primeiro vertical slice GLPI + Tactical

Status: **concluída** (2026-09-11). Design técnico apenas; nenhum código
implementado. A primeira tarefa de implementação é `T-003`.

## Resultado esperado

Plano executável para o primeiro slice do MVP (Fases 1–3) sobre as conversas
WhatsApp existentes `(session_id, chat_jid)`, sem criar `conversation_id` e sem
tocar no Flow Builder.

## Restrições confirmadas no código (base do design)

Padrões reais verificados (ver também `docs/ARCHITECTURE.md`):

- **Store**: `type xStore struct{ db *sql.DB }` + `newXStore(ctx, db) (*xStore, error)`
  que roda `CREATE TABLE IF NOT EXISTS` no boot. Ex.: `tagstore.go`.
- **Migrations aditivas**: não há arquivos SQL versionados. Cada store cria o
  schema no boot e faz *best-effort* `ALTER TABLE ADD COLUMN` (padrão de
  `queuestore.go`/`chatmetastore.go`). `cmd/migrate` só copia SQLite→MariaDB.
  O schema roda em SQLite **e** MariaDB, então o DDL deve ser compatível com
  ambos (usar `INTEGER`/`TEXT`, `IF NOT EXISTS`, `ON CONFLICT`).
- **Rotas**: `s.registerXRoutes(mux)` agregado em `httpapi.go`; handlers usam
  `s.requireAuth(...)`; padrão de path `/{param}`.
- **Idempotência**: `ON CONFLICT(...) DO UPDATE` / `INSERT OR IGNORE` /
  `INSERT OR REPLACE` + índice único na chave natural (ex.: `transcripts(scope,
  ref_id)`, `campaign_targets(campaign_id, jid)`).
- **HTTP externo**: `&http.Client{Timeout: N*time.Second}` + `http.NewRequestWithContext`;
  base URL/token via env `WACALLS_*` (padrão de `flowbridge.go`, `transcriber.go`,
  `billingapi.go`).
- **Feature flag**: env `WACALLS_*` para o backend + `features map[string]bool`
  do plano, exposto ao client em `GET /api/settings/options`
  (`activePlanLimits` em `settingsapi.go`).
- **Multitenancy SaaS já existe**: tabelas de domínio carregam `owner_id` +
  `tenant_id` = empresa (ex.: `scheduled_messages`, `quick_replies`). Secretaria
  ≠ tenant (D-009). As novas tabelas seguem o mesmo isolamento por empresa.
- **Client**: a conversa vive em `client/src/pages/ChatsPage.tsx` com
  `client/src/services/chats.ts`. Serviços seguem um arquivo por domínio em
  `client/src/services/`.

## Modelos (schema — SQLite/MariaDB compatível)

### `device_bindings` (vínculo do equipamento GLPI ↔ Tactical)

```text
id                  TEXT PRIMARY KEY            -- uuid
owner_id            TEXT NOT NULL DEFAULT ''    -- empresa (SaaS), não secretaria
tenant_id           TEXT NOT NULL DEFAULT ''    -- empresa raiz (SaaS)
hostname            TEXT NOT NULL
hostname_normalized TEXT NOT NULL               -- upper+trim; UNIQUE por tenant
tactical_agent_id   TEXT NOT NULL DEFAULT ''
glpi_computer_id    TEXT NOT NULL DEFAULT ''
tactical_client_id  TEXT NOT NULL DEFAULT ''
tactical_site_id    TEXT NOT NULL DEFAULT ''
sector_code         TEXT NOT NULL DEFAULT ''
patrimonio          TEXT NOT NULL DEFAULT ''
match_status        TEXT NOT NULL DEFAULT 'pending'
                    -- pending|matched|conflict|missing_glpi|missing_tactical|disabled
last_verified_at    INTEGER NOT NULL DEFAULT 0
created_at          INTEGER NOT NULL
updated_at          INTEGER NOT NULL
```

Índice único: `(tenant_id, hostname_normalized)` — garante uma correspondência
por hostname e habilita upsert idempotente da sincronização. Duplicidade real na
origem vira `match_status='conflict'` (não sobrescreve silenciosamente).

### `support_requests` (solicitação/ticket ligada à conversa atual)

```text
id                TEXT PRIMARY KEY              -- uuid
owner_id          TEXT NOT NULL DEFAULT ''
tenant_id         TEXT NOT NULL DEFAULT ''
session_id        TEXT NOT NULL                -- conversa WhatsApp atual
chat_jid          TEXT NOT NULL                -- (NÃO cria conversation_id)
requester_name    TEXT NOT NULL DEFAULT ''
source_device_id  TEXT NOT NULL DEFAULT ''     -- device_bindings.id (origem)
target_device_id  TEXT NOT NULL DEFAULT ''     -- device_bindings.id (afetado)
category_id       TEXT NOT NULL DEFAULT ''
glpi_ticket_id    TEXT NOT NULL DEFAULT ''
sync_state        TEXT NOT NULL DEFAULT 'not_linked'
                  -- not_linked|pending|linked|sync_error
error_detail      TEXT NOT NULL DEFAULT ''
attempts          INTEGER NOT NULL DEFAULT 0
next_retry_at     INTEGER NOT NULL DEFAULT 0
idempotency_key   TEXT NOT NULL                -- UNIQUE
status            TEXT NOT NULL DEFAULT 'open'  -- open|closed
created_at        INTEGER NOT NULL
updated_at        INTEGER NOT NULL
```

Índice único: `(idempotency_key)`. A chave é derivada de
`sha1(tenant_id | session_id | chat_jid | client_token)`, onde `client_token`
vem do botão "Criar chamado" (um por intenção de abertura). Repetir a submissão
não cria ticket duplicado.

`source_device` e `target_device` separados (D-003): a pessoa pode abrir chamado
para outro equipamento.

## Migrations

- `newDeviceBindingStore(ctx, db)` e `newSupportStore(ctx, db)` criam as tabelas e
  índices com `CREATE TABLE/INDEX IF NOT EXISTS` no boot.
- Evoluções futuras: `ALTER TABLE ... ADD COLUMN` best-effort (ignorar
  "duplicate column"). Nada destrutivo. Nenhuma tabela existente é alterada.

## Serviços de integração (novos pacotes `internal/`)

Seguindo `internal/wa`, `internal/voip`:

```text
internal/glpi/       Client{base,token,http}; New(cfg); erros tipados
  - CreateTicket(ctx, in) (ticketID, error)      -- idempotente via chave externa
  - GetTicket(ctx, id) (Ticket, error)
  - FindComputerByName(ctx, hostname) (computerID, error)
internal/tactical/   Client{base,token,http}; New(cfg); erros tipados
  - GetAgentStatus(ctx, agentID) (Status, error) -- online/offline, hostname, ip
  - SearchAgents(ctx, query) ([]Agent, error)
```

- Cada cliente: `&http.Client{Timeout}` (15–25s), `NewRequestWithContext`, base
  URL/token de env, erros tipados `ErrUnavailable` / `ErrNotFound` / `ErrAuth`.
- No `cmd/server`, interfaces mínimas `glpiClient` / `tacticalClient` para
  permitir **mocks** nos testes (sem chamar APIs reais).
- **Somente leitura** em Tactical; **create/get** em GLPI. Sem scripts/reboot/remoto.

## Endpoints (novos — não alteram as rotas de chat existentes)

Registrados por `s.registerSupportRoutes(mux)`, todos `requireAuth` e com escopo
por sessão/tenant, espelhando `messageapi.go`:

```text
GET  /api/sessions/{sid}/chats/{jid}/support
     -> painel: support_request atual + device vinculado + status Tactical (cache)
POST /api/sessions/{sid}/chats/{jid}/support/ticket
     -> cria/associa ticket GLPI (idempotente por idempotency_key)
POST /api/sessions/{sid}/chats/{jid}/support/device
     -> vincula/troca target_device (auditado)
GET  /api/support/devices?query=...
     -> busca device_bindings por hostname normalizado/unidade
GET  /api/support/devices/{id}/tactical
     -> status Tactical read-only (cache TTL curto)
GET  /api/support/devices            (admin)  -> lista/inventário
GET  /api/support/conflicts          (admin)  -> match_status in (conflict,missing_*)
POST /api/support/devices/{id}/confirm (admin) -> confirmação manual do vínculo
```

Preservação: `GET/POST /api/sessions/{sid}/chats/...` de `messageapi.go`
permanecem intactas; o WhatsApp continua igual.

## Componentes (client)

- `client/src/services/support.ts` — cliente HTTP dos endpoints acima
  (espelha `services/chats.ts`).
- `client/src/components/domain/support/SupportPanel.tsx` — painel lateral
  retrátil, com subcomponentes: `TicketCard`, `DeviceCard`, `DeviceSearch`,
  `TacticalStatusBadge`.
- Integração **aditiva** em `ChatsPage.tsx`: montar `<SupportPanel sessionId jid/>`
  na visão da conversa selecionada, atrás do flag `features.support`. Sem alterar
  a lógica de chat/SSE existente.

## Feature flags

- Backend: `WACALLS_SUPPORT_ENABLED` (mestre), `WACALLS_GLPI_BASE_URL`,
  `WACALLS_GLPI_TOKEN`, `WACALLS_TACTICAL_BASE_URL`, `WACALLS_TACTICAL_TOKEN`.
  Com o mestre desligado, `registerSupportRoutes` responde `503 disabled` e o
  runner de retry não roda.
- Client: expor `support: <bool>` no mapa `features` de `GET /api/settings/options`
  (pequena adição em `handleGetOptions`), para o painel aparecer/sumir sem
  afetar o chat (critério 10 do MVP).
- `.env.example`: adicionar os nomes acima com valores fictícios.

## Idempotência

- **Ticket**: `support_requests.idempotency_key` UNIQUE; criação via
  `INSERT ... ON CONFLICT(idempotency_key) DO NOTHING` + re-`SELECT`; se já houver
  `glpi_ticket_id`, retorna o existente. GLPI `CreateTicket` também usa uma
  referência externa (ex.: `id` do support_request) para não duplicar no GLPI.
- **device_bindings**: upsert `ON CONFLICT(tenant_id, hostname_normalized)`.
- **Sincronização** repetida é segura por construção (mesmas chaves naturais).

## Tratamento de falhas

- Timeout em todos os clientes externos; erros tipados mapeados para HTTP
  (503 quando `ErrUnavailable`, 404 quando `ErrNotFound`).
- Falha do GLPI ao criar: **não** perde a solicitação local — persiste
  `sync_state='sync_error'` + `error_detail`, `attempts++`, `next_retry_at`.
- Retry mínimo: um goroutine ticker no servidor (padrão de `campaignrunner.go`)
  varre `support_requests` com `sync_state='sync_error'` e `next_retry_at<=now`,
  re-tenta a criação idempotente, com backoff e teto de `attempts` (ex.: 5).
  Botão "Tentar novamente" no painel sempre disponível.
- Tactical read-only é best-effort: cache com TTL curto (30–60s, in-memory ou
  `internal/cache`); quando o status estiver velho, o painel indica
  "desatualizado". Nunca bloqueia o chat.
- **Sem** retry automático de ação remota/reboot/script (fora do MVP).

## Testes (offline; sem APIs reais)

- `cmd/server/hostname_test.go` — normalização (case-fold, trim), correspondência
  **exata**, rejeição de aproximação, parse do padrão `{SEC}-{UNI}-{SETOR}-{NN}`
  e tolerância a legados como alias (ver `docs/HOSTNAMES.md`).
- `cmd/server/devicebindingstore_test.go` — upsert idempotente, unicidade por
  `(tenant_id, hostname_normalized)`, transição para `conflict`.
- `cmd/server/supportstore_test.go` — idempotency_key único (não duplica ticket),
  transições `not_linked→pending→linked→sync_error`, isolamento por tenant.
- `internal/glpi/client_test.go`, `internal/tactical/client_test.go` — `httptest`
  mock: sucesso, timeout, 404, 401; mapeamento de erros tipados; idempotência do
  CreateTicket.
- Padrão de teste com SQLite em memória já existe (`db_test.go`,
  `sessionstore_test.go`).

## Arquivos afetados

Criar:

```text
cmd/server/hostname.go                     (funções puras: normalize/parse/match)
cmd/server/devicebindingstore.go
cmd/server/supportstore.go
cmd/server/supportapi.go                   (registerSupportRoutes + handlers)
cmd/server/support_integration.go          (interfaces glpiClient/tacticalClient,
                                            wiring, retry runner, cache Tactical)
internal/glpi/client.go, internal/glpi/types.go
internal/tactical/client.go, internal/tactical/types.go
cmd/server/hostname_test.go
cmd/server/devicebindingstore_test.go
cmd/server/supportstore_test.go
internal/glpi/client_test.go
internal/tactical/client_test.go
client/src/services/support.ts
client/src/components/domain/support/SupportPanel.tsx (+ subcomponentes)
.env.example                               (nomes WACALLS_SUPPORT_*/GLPI/TACTICAL)
```

Alterar (aditivo):

```text
cmd/server/server.go        -> instanciar stores/clients/runner; setar s.support,
                               s.deviceBindings, s.glpi, s.tactical
cmd/server/server.go (struct server) -> novos campos
cmd/server/httpapi.go       -> s.registerSupportRoutes(mux)
cmd/server/settingsapi.go   -> handleGetOptions expõe features["support"]
client/src/pages/ChatsPage.tsx -> montar <SupportPanel/> (atrás do flag)
```

**Não** tocar: `flowexec.go`, `flowexec_chat.go`, `flowbridge.go`, `flowapi.go`,
`flowstore.go`; as rotas/handlers de chat em `messageapi.go`; o modelo
`(session_id, chat_jid)`. **Não** criar `conversation_id`. WhatsApp preservado.

## Ordem de implementação (fatias pequenas)

1. `T-003` — `device_bindings` store + `hostname.go` (normalize/match) + testes.
   Fundação offline, sem APIs externas, 100% testável.
2. T-004 — cliente `internal/glpi` + `internal/tactical` com mocks/testes.
3. T-005 — `support_requests` store + `supportapi.go` (criação idempotente de
   ticket) atrás do flag.
4. T-006 — `SupportPanel` no client + `features["support"]`.
5. T-007 — runner de retry + cache Tactical + auditoria.

## Confirmações exigidas pelos critérios de aceitação

- Mapa de arquivos com justificativa: acima.
- Schemas revisados contra `docs/ARCHITECTURE.md`: alinhados (conversation vs
  support_request vs device_binding).
- Auth/idempotência/retry/flag descritos: acima.
- Flow Builder e identidade de conversa **não** serão alterados: declarado acima.
- Decisões novas: registradas em `docs/DECISIONS.md` (D-013, D-014).

## Resultado obtido (2026-09-11)

Design concluído. Nenhum código escrito. Próxima ação: `T-003`
(`docs/tasks/T-003-DEVICE-BINDINGS-STORE.md`).
