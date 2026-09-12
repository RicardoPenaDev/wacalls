# Estado atual

Atualizado em: 2026-09-12
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa ativa: nenhuma em execução — próxima pronta é `T-004`.

## Objetivo atual

Fundação offline do domínio de suporte pronta: store `device_bindings` +
normalização/parse/match de hostname, com testes. Próximo slice é `T-004`
(clientes `internal/glpi` e `internal/tactical` com mocks).

## Concluído recentemente

- `T-001` — inventário/baseline confirmado no código.
- `T-002` — plano técnico do slice (modelos, endpoints, clientes `internal/`,
  flags, idempotência, falhas, mapa de arquivos). Decisões D-013/D-014.
- `T-B001` — **concluída**. Pacote `internal/voip/media` recuperado do backup e
  versionado (84 arquivos); `.gitignore` linha 20 `media/` → `/media/`. Commit
  `fe416df` (publicado como `f720b0b` após rebase).
- `T-003` — **concluída**. `cmd/server/hostname.go` (funções puras
  `normalizeHostname`/`parseHostname`/`hostnamesMatch`) e
  `cmd/server/devicebindingstore.go` (store `device_bindings`: `Upsert`
  idempotente por `(tenant_id, hostname_normalized)`, `Get`, `FindByHostname`,
  `Search`), com testes SQLite. Sem rotas, integrações externas ou client.

## Em andamento

- Nada em execução.

## Próxima tarefa pronta

**`T-004 — clientes internal/glpi + internal/tactical`** com mocks/testes
(`httptest`): `CreateTicket`/`GetTicket`/`FindComputerByName` (GLPI, create/get)
e `GetAgentStatus`/`SearchAgents` (Tactical, somente leitura). Erros tipados
`ErrUnavailable`/`ErrNotFound`/`ErrAuth`; base URL/token via env `WACALLS_*`.
Sequência da Fase 1: T-003 → **T-004** → T-005 → T-006 → T-007 (ver `T-002`).

## Decisões e limitações da T-003

- Store exige `tenant_id` não-vazio (`ErrMissingTenant`) em `Upsert`,
  `FindByHostname` e `Search` — isolamento por empresa (D-009/D-013). `Get` é
  por `id` global.
- `Upsert` **enriquece** atomicamente: valor não-vazio vence, vazio nunca limpa
  um id gravado; `id`/`created_at` preservados no conflito. `match_status`:
  inserção vazia → `pending`; atualização vazia → **mantém** o status atual;
  valor explícito (incl. `pending`) é aplicado. Validado contra o conjunto
  `pending|matched|conflict|missing_glpi|missing_tactical|disabled`.
- Erros distinguíveis: `ErrDeviceBindingNotFound`, `ErrInvalidHostname`,
  `ErrMissingTenant`, `ErrDeviceBindingConflict` (id reusado com outra chave),
  `ErrInvalidMatchStatus` (status fora do conjunto documentado).
- **Deferido** (D-013): detectar duplicidade real de origem e marcar
  `match_status='conflict'` é do sync (T-004+), não do store.

## Bloqueios

- Nenhum bloqueio de build.
- `conversation_id` (Fase 4A) é pré-requisito do portal (Fase 4B), não do MVP.

## Sincronização com origin/main (verificado 2026-09-12)

- Remote `https://github.com/RicardoPenaDev/wacalls.git`; branch `main`.
- `origin/main` = `37a1953`, sincronizado após rebase; `README-BACKUP.txt`
  presente. Recuperação do baseline VoIP e docs já **publicadas**.
- Commit local **novo** desta sessão: `feat(support): add device binding store
  and hostname normalization` — **não enviado** (push aguarda autorização).

## Ambiente e comandos de validação

- SO: Windows 11 (`Windows_NT`, 10.0.26200), x64/AMD64.
- Toolchain: **Go 1.26.4 portátil** em `D:/fabrica/WaCalls/toolchains/go1.26.4`
  (fora do repositório, não versionado). Caches em `D:/fabrica/WaCalls/toolchains/`
  (`gopath`, `gocache`), `GOTOOLCHAIN=local`.
- O wrapper de shell bloqueia o token `go`; executar via
  `pwsh -NoProfile -Command "... ; go <cmd>"` com `GOROOT`/`GOPATH`/`GOCACHE`/
  `GOMODCACHE` exportados e `$env:Path` incluindo `<GOROOT>\bin`.

## Baseline de validação (2026-09-12)

- `go version` → `go1.26.4 windows/amd64` ✅
- `go build ./...` → **OK** ✅ (exit 0).
- `go test ./...` → **OK** ✅ (exit 0): `ok cmd/server`, `ok internal/voip/media`,
  `ok internal/voip/media/mlow`, `ok internal/voip/call`, `ok .../signaling`,
  `ok .../transport`; demais pacotes sem testes.

## Fatos técnicos confirmados

- Módulo `wacalls`, `go 1.26.4`.
- Conversa identificada por `(session_id, chat_jid)`; **sem** `conversation_id`.
- Rotas de chat em `cmd/server/messageapi.go` (não alterar):
  `GET /api/sessions/{sid}/chats`, `.../{jid}/messages`, `POST .../{jid}/send`.
- Store: `newXStore(ctx, db)` cria schema no boot; migrations aditivas =
  `ALTER TABLE ADD COLUMN` best-effort. Roda em SQLite e MariaDB.
- Rotas agregadas por `s.registerXRoutes(mux)` em `httpapi.go` com `requireAuth`.
- Idempotência: `ON CONFLICT ... DO UPDATE` / `INSERT OR IGNORE` + índice único.
- Feature flag: env + `features map[string]bool` em `GET /api/settings/options`.
- Multitenancy SaaS já existe (tenant = empresa). Secretaria ≠ tenant (D-009).

## Arquivos da Fase 1 (mapa completo em T-002)

- Criar: `cmd/server/hostname.go`, `devicebindingstore.go`, `supportstore.go`,
  `supportapi.go`, `support_integration.go`; `internal/glpi/*`, `internal/tactical/*`;
  testes; `client/src/services/support.ts`, `.../components/domain/support/SupportPanel.tsx`.
- Alterar (aditivo): `server.go` (wiring), `httpapi.go` (registerSupportRoutes),
  `settingsapi.go` (`features["support"]`), `ChatsPage.tsx` (montar painel).
- **Não** alterar: `flowexec*.go`, `flowbridge.go`, `flow{api,store}.go`,
  rotas/handlers de `messageapi.go`, o modelo `(session_id, chat_jid)`.
