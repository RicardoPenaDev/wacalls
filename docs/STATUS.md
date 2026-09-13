# Estado atual

Atualizado em: 2026-09-13
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa atual: `T-005` — CONCLUÍDA LOCALMENTE (persistência, GetTicket/SupportService, HTTP API e wiring finalizados; ajustes de auditoria aplicados; pendente revisão).
Push e integrações de runtime ainda não autorizados.

## Objetivo imediato

Revisão final do commit local da etapa HTTP API e wiring da T-005 antes de autorização de push.

```text
fundação T-005 (OK) → GetTicket/SupportService (OK) → HTTP API/rotas (OK) → ajustes de auditoria (OK)
```

## Concluído

- `T-001` — inventário/baseline do código.
- `T-002` — plano técnico do slice; decisões D-013/D-014.
- `T-B001` — pacote `internal/voip/media` recuperado e versionado.
- `T-003` — normalização de hostname e store `device_bindings`, com SQLite.
- `T-004` — clientes `internal/glpi` e `internal/tactical`, testes offline.
- `chore` — `.gitattributes` multiplataforma com política explícita de EOL.
- `T-B002` — harness MariaDB descartável validado 100% em SQLite e MariaDB 11.4.
- `T-005 (fundação de persistência)` — DDL, integridade referencial com FK composta, 23 testes em SQLite e MariaDB 11.4 publicados em `origin/main`.
- `T-005 (GLPI GetTicket + SupportService / Orquestrador)`:
  - `internal/glpi`: `GetTicket` em `client.go` e `Ticket` em `types.go`, validação de ID numérico positivo, endpoint exato v2.3, renovação OAuth 401 e redação de credenciais.
  - `cmd/server/support_content.go`: sanitização HTML e conversão de quebras de linha para `<br>`.
  - `cmd/server/supportservice.go`: ciclo de vida fechado, CAS, detecção de replays, retry concorrente e reconciliação segura com verificação de `external_id`.
  - Ajuste de isolamento: `CreateTicket` atualizado para consulta estritamente tenant-scoped `bindings.GetForTenant(ctx, in.TenantID, bID)`, eliminando a consulta residual legada por ID. Método `Get` removido da interface `deviceBindingStoreBackend`.
- `T-005 (HTTP API, Rotas e Wiring de Boot)`:
  - `cmd/server/supportapi.go`: exatamente os 8 rotas HTTP mínimas sob `WACALLS_SUPPORT_ENABLED`, checagem de tenant isolation (`WHERE id = ? AND tenant_id = ?`), validação de acesso à conversa `(sid, jid)`, Content-Type `application/json`, CAS de retry e reconciliação administrativa. Rota `/events` removida da exposição HTTP para manter escopo dos 8 endpoints mínimos.
  - Reconcile administrativo: exige `currentUser.IsAdmin()`, busca request por `(id, tenant_id)` (outro tenant = 404), permitindo ao administrador de uma empresa SaaS reconciliar qualquer chamado de seu tenant sem exigir `userCanAccessSession`.
  - Detecção de payload excessivo: leitura limitada a `maxBytes+1` com `countedReader`/probe, rejeitando excesso com `400 invalid_request` sem alocação ilimitada (testes explícitos para limite exato e limite + 1).
  - `cmd/server/support_api_types.go`: DTOs públicos sem vazamento de `processing_token` ou `payload_fingerprint`. Removido DTO público não utilizado de eventos.
  - `cmd/server/support_config.go` e `cmd/server/server.go`: configuração via `WACALLS_*`, validação rigorosa de GLPI/Tactical, wiring e recuperação síncrona de órfãos no boot (`RecoverOrphanedProcessing`).
  - `cmd/server/settingsapi.go`: flags `features.support` e `features.tactical` expostas em `GET /api/settings/options`.
  - `cmd/server/httpapi.go`: inclusão de `Idempotency-Key` no CORS.
  - `cmd/server/supportapi_test.go` e `cmd/server/supportservice_test.go`: suítes de testes 100% offline cobrindo flag desabilitada (503), falta de auth (401), IDOR (404), sessão não permitida (403), criação/replay/conflito de chave, disputa no retry (202/409), reconciliação remota (404/422/200), Content-Type/documentos múltiplos (415/400), limite de body (16384 vs 16385), não registro de `/events` (404), degradação graciosa Tactical, ausência de tokens no JSON, recuperação no startup e rejeição de binding cross-tenant.

## Bloqueios

- Vínculo nativo Ticket↔Computer: indisponível na API GLPI v2.3; mantido contexto textual.
- `conversation_id`: fora do MVP; bloqueia somente portal futuro.
- `T-006 (Frontend)`: não iniciada; bloqueada até conclusão e publicação da T-005.
- Push: bloqueado até autorização explícita do usuário.

## Estado Git do checkpoint

- HEAD local: commit pendente da etapa HTTP API + wiring (incorporando correções de auditoria).
- Nenhum push realizado ou autorizado.

## Próximo passo

Revisão final da T-005 HTTP/wiring e autorização para push.

## Ambiente preservado

- Windows 11 x64 (`OhMyPi`).
- Go 1.26.4 portátil em `D:/fabrica/WaCalls/toolchains/`.
- Docker Desktop 4.90.0 (WSL2 backend) restrito ao harness de testes locais.
- Nenhuma credencial real no repositório.

## Não tocar nesta etapa

- `client/`, portal/agente Windows e Flow Builder;
- rotas existentes de `messageapi.go` e modelo `(session_id, chat_jid)`;
- automação remota Tactical, retry automático ou worker GLPI;
- chamadas externas reais ou push até autorização.
