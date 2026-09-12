# Estado atual

Atualizado em: 2026-09-12
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa ativa: **nenhuma**. `T-004` concluída. `T-005` não possui especificação
própria e não foi iniciada. Nenhuma credencial é armazenada no repositório.

## Objetivo atual

Fundação offline do domínio de suporte inclui store/normalização T-003 e
clientes HTTP GLPI/Tactical T-004, todos testados. Próxima etapa: criar e
revisar a especificação própria da T-005 antes de qualquer implementação.

## Concluído recentemente

- `T-001` — inventário/baseline confirmado no código.
- `T-002` — plano técnico do slice (modelos, endpoints, clientes `internal/`,
  flags, idempotência, falhas, mapa de arquivos). Decisões D-013/D-014.
- `T-B001` — **concluída**. Pacote `internal/voip/media` recuperado do backup e
  versionado (84 arquivos); `.gitignore` linha 20 `media/` → `/media/`.
- `T-003` — **concluída**. `cmd/server/hostname.go` (funções puras
  `normalizeHostname`/`parseHostname`/`hostnamesMatch`) e
  `cmd/server/devicebindingstore.go` (store `device_bindings`: `Upsert`
  idempotente por `(tenant_id, hostname_normalized)`, `Get`, `FindByHostname`,
  `Search`), com testes SQLite. Sem rotas, integrações externas ou client.
- `T-004` — **concluída**. Clientes `internal/glpi` (API v2.3, OAuth2 password
  grant, Computer e criação de Ticket) e `internal/tactical` (agentes
  read-only), erros tipados, limites de body, redirects seguros e testes
  `httptest`. Correções concluídas: validação estrita de `agent_id` contra path
  traversal; `tokenFlight` compartilhando falha OAuth entre waiters; testes de
  `Retry-After` HTTP-date, limite exato de body e deadlines. Build e testes
  passam; testes não realizaram chamadas reais.

## Em andamento

- Nenhuma tarefa em implementação. T-005 não foi iniciada.

## Próxima etapa planejada

Não existe `docs/tasks/T-005-*.md`. Criar e revisar uma especificação própria da
T-005 antes de implementar `support_requests`, API ou wiring. Sequência da Fase
1: T-003 → T-004 → **T-005** → T-006 → T-007 (ver `T-002`).

## Decisões e limitações da T-004

- GLPI usa apenas `/api.php/token` e `/api.php/v2.3`; API legada e
  `LinkComputerToTicket` não fazem parte do cliente.
- Vínculo nativo Ticket↔Computer permanece bloqueado pela ausência de rota
  v2.3. T-005 registrará hostname/Computer ID como contexto textual.
- Normalização privada dos clientes duplica somente `TrimSpace` + `ToUpper`;
  compartilhar código exigiria alterar a fronteira T-003. T-005 revalida.
- Tactical expõe somente leitura; sem script, terminal, reboot ou acesso remoto.
- TLS verification permanece habilitada e não há opção insecure.
- `agent_id` Tactical aceita somente ASCII alfanumérico, `_` e `-`, até 128
  bytes; entrada inválida retorna `ErrBadRequest` sem requisição.
- Uma falha de aquisição OAuth é compartilhada pelo grupo concorrente; nova
  chamada após o grupo pode tentar novamente, sem cooldown global.

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
- Vínculo nativo Ticket↔Computer permanece bloqueado pela ausência de rota v2.3;
  não bloqueou a T-004.
- `conversation_id` (Fase 4A) é pré-requisito do portal (Fase 4B), não do MVP.

## Próximo passo

Criar e revisar uma especificação própria da T-005 antes de implementar
qualquer parte dessa tarefa.

## Ambiente e comandos de validação

- SO: Windows 11 (`Windows_NT`, 10.0.26200), x64/AMD64.
- Toolchain: **Go 1.26.4 portátil** em `D:/fabrica/WaCalls/toolchains/go1.26.4`
  (fora do repositório, não versionado). Caches em `D:/fabrica/WaCalls/toolchains/`
  (`gopath`, `gocache`), `GOTOOLCHAIN=local`.
- Comandos Go usam diretamente `<GOROOT>/bin/go.exe` com `GOROOT`, `GOPATH`,
  `GOCACHE`, `GOMODCACHE` e `GOTOOLCHAIN=local`.

## Validação da T-004 corrigida (2026-09-12)

- `gofmt -l internal/glpi internal/tactical` → sem saída.
- `go vet ./internal/glpi/... ./internal/tactical/...` → **OK**.
- `go test ./internal/glpi/... -count=20` → **OK**.
- `go test ./internal/tactical/... -count=20` → **OK**.
- `go build ./...` → **OK**.
- `go test ./...` → **OK** (`cmd/server` e todos os pacotes testados).
- Race detector não executado: `-race requires cgo`; `CGO_ENABLED=0` e `gcc`
  ausente no PATH. Nenhuma toolchain adicional foi instalada.
- `git diff --check origin/main..HEAD` → **OK**.

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
