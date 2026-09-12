# Estado atual

Atualizado em: 2026-09-12
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa ativa: nenhuma em execução — próxima pronta é `T-003` (desbloqueada).

## Objetivo atual

Baseline de VoIP recuperado e build/testes verdes. `T-003` (device_bindings store
+ normalização de hostname) está pronta para iniciar em sessão dedicada.

## Concluído recentemente

- `T-001` — inventário/baseline confirmado no código.
- `T-002` — plano técnico do slice (modelos, endpoints, clientes `internal/`,
  flags, idempotência, falhas, mapa de arquivos). Decisões D-013/D-014.
- `T-B001` — **concluída**. Pacote `internal/voip/media` recuperado do backup
  `D:/fabrica/WaCalls/recovery/wacalls-chat-backup-completo.zip` e **versionado**
  (84 arquivos, com `mlow/` e `testdata/`). Causa-raiz corrigida: `.gitignore`
  linha 20 passou de `media/` (engolia o pacote-fonte em qualquer profundidade)
  para `/media/` (ancorado à raiz — só uploads/gravações em runtime). Commit
  `fe416df`.

## Em andamento

- Nada em execução.

## Próxima tarefa pronta

**`T-003 — device_bindings store + normalização de hostname`**
(`docs/tasks/T-003-DEVICE-BINDINGS-STORE.md`). Desbloqueada: `cmd/server` volta a
compilar e testar. É uma tarefa offline (SQLite em memória; sem GLPI/Tactical,
WhatsApp ou client). Sequência da Fase 1: T-003 → T-004 → T-005 → T-006 → T-007
(detalhe em `T-002`).

## Bloqueios

- Nenhum bloqueio de build (T-B001 resolvida).
- `conversation_id` (Fase 4A) é pré-requisito do portal (Fase 4B), não do MVP.

## Sincronização com origin/main (verificado 2026-09-12, pós-`git fetch`)

- Remote: `https://github.com/RicardoPenaDev/wacalls.git` (repo `wacalls`).
- Branch `main`; árvore de trabalho **limpa**; **sem rebase em andamento**.
- **Divergente: ahead 2, behind 1.**
  - Locais, ainda não enviados: `fe416df` (restore media) e `028b887`
    (docs: track project documentation).
  - Remoto, ainda não integrado: `d11a57b docs: adiciona instrucoes de backup e
    restauracao` — adiciona `README-BACKUP.txt` (**preservado** no `origin/main`;
    ausente localmente só porque o commit ainda não foi integrado, sem perda).
- **Push pendente e não autorizado.** Integrar `d11a57b` antes de enviar (rebase
  ou merge — decisão do mantenedor; sem force-push, sem `reset --hard`).
- `gh auth status`: token **inválido** — re-login necessário antes de push/PR.

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
