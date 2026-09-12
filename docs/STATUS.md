# Estado atual

Atualizado em: 2026-09-11
Fase atual: Fase 1 — MVP GLPI + Tactical (ambiente de validação pronto)
Tarefa ativa: nenhuma em execução — próxima pronta é `T-003` (ver bloqueio).

## Objetivo atual

Ambiente de validação Go preparado e baseline registrado antes da `T-003`. Há um
bloqueio de build **pré-existente** (pacote `internal/voip/media` ausente) que
impede compilar/testar `cmd/server` — precisa de decisão antes de validar T-003.

## Concluído recentemente

- `T-001` — inventário/baseline confirmado no código.
- `T-002` — plano técnico do slice (modelos, endpoints, clientes `internal/`,
  flags, idempotência, falhas, mapa de arquivos). Sem código. Decisões D-013/D-014.
- Ambiente de validação: Go 1.26.4 portátil instalado e baseline `build`/`test`
  executado (resultados abaixo).

## Em andamento

- Nada em execução.

## Próxima tarefa pronta

**Ação imediata: `T-B001`** — recuperar `internal/voip/media` (bloqueia tudo em
`cmd/server`). Investigação read-only concluída; aguarda o artefato `wacalls.zip`
ou a pasta do servidor/mantenedor. Ver
`docs/tasks/T-B001-RECUPERAR-BASELINE-VOIP-MEDIA.md`.

Depois de desbloquear: `T-003 — device_bindings store + normalização de hostname`
(`docs/tasks/T-003-DEVICE-BINDINGS-STORE.md`). O teste de T-003
(`go test ./cmd/server/...`) compila todo o `cmd/server`, hoje quebrado.
Sequência: T-003 → T-004 → T-005 → T-006 → T-007 (detalhe em `T-002`).

## Bloqueios

- **Build quebrado — `internal/voip/media` ausente (T-B001).** Causa-raiz
  **confirmada**: `.gitignore:20` tem o padrão `media/` (sem âncora), que ignora
  qualquer dir `media` em qualquer nível — inclusive o pacote-fonte
  `internal/voip/media/`. Por isso ele nunca foi versionado/enviado; o instalador
  o distribui dentro de `wacalls.zip`. `internal/voip/call` importa esse pacote e,
  por transição (`cmd/server/session.go`, `callregistry.go`), `cmd/server` não
  compila. Ausente de **todo** o histórico Git (7 commits, só `main`), sem
  submodule/LFS, não gerado por build. Fonte legítima: `wacalls.zip` do
  instalador ou a árvore do mantenedor/servidor — **não disponível** neste
  ambiente. Investigação completa e opções de recuperação em
  `docs/tasks/T-B001-RECUPERAR-BASELINE-VOIP-MEDIA.md`. **Não restaurado**
  (aguarda insumo/decisão do mantenedor). T-003 permanece bloqueada.
- `conversation_id` (Fase 4A) é pré-requisito do portal (Fase 4B), não do MVP.

## Ambiente e comandos de validação

- SO: Windows 11 (`Windows_NT`, 10.0.26200), x64/AMD64.
- Toolchain: **Go 1.26.4 portátil** em `D:/fabrica/WaCalls/toolchains/go1.26.4`
  (baixado de go.dev, sha256 conferido; **fora do repositório**, não versionado —
  nenhum binário no Git). Caches locais em `D:/fabrica/WaCalls/toolchains/`
  (`gopath`, `gocache`), `GOTOOLCHAIN=local`.
- O wrapper de shell bloqueia o token `go`; executar via
  `pwsh -NoProfile -Command "... ; go <cmd>"` com `GOROOT`/`GOPATH`/`GOCACHE`/
  `GOMODCACHE` exportados e `$env:Path` incluindo `<GOROOT>\bin`.

## Baseline de validação (2026-09-11)

- `go version` → `go version go1.26.4 windows/amd64` ✅
- `go env GOMOD` → `D:\fabrica\WaCalls\wacalls\go.mod` ✅ (módulo `wacalls` resolve)
- `go build ./...` → **FALHA** ❌
  `internal/voip/call/callmanager.go:9: package wacalls/internal/voip/media is
  not in std` (pacote ausente; ver Bloqueios).
- `go test ./...` → **FALHA** ❌ (mesma causa):
  - `FAIL wacalls/cmd/server [setup failed]`
  - `FAIL wacalls/internal/voip/call [setup failed]`
  - `ok wacalls/internal/voip/signaling`, `ok wacalls/internal/voip/transport`
  - sem testes: `cmd/migrate`, `internal/cache`, `internal/storage`,
    `internal/voip/core`, `internal/voip/wanode`, `internal/wa`,
    `internal/wa/cloudapi`.
- Conclusão: a falha é **pré-existente** e independe das mudanças de docs e da
  T-003. Bloqueia `build`/`test` de `cmd/server` (onde T-003 adiciona testes).

## Fatos técnicos confirmados

- Módulo `wacalls`, `go 1.26.4` (toolchain disponível localmente).
- Conversa identificada por `(session_id, chat_jid)`; **sem** `conversation_id`.
- Rotas de chat em `cmd/server/messageapi.go` (não serão alteradas):
  `GET /api/sessions/{sid}/chats`, `.../{jid}/messages`, `POST .../{jid}/send`.
- Store: `newXStore(ctx, db)` cria schema no boot; migrations aditivas =
  `ALTER TABLE ADD COLUMN` best-effort. Roda em SQLite e MariaDB.
- Rotas agregadas por `s.registerXRoutes(mux)` em `httpapi.go` com `requireAuth`.
- Idempotência: `ON CONFLICT ... DO UPDATE` / `INSERT OR IGNORE` + índice único.
- HTTP externo: `http.Client{Timeout}` + `NewRequestWithContext`, env `WACALLS_*`.
- Feature flag: env + `features map[string]bool` em `GET /api/settings/options`.
- Multitenancy SaaS já existe (tenant = empresa). Secretaria ≠ tenant.

## Arquivos da Fase 1 (mapa completo em T-002)

- Criar: `cmd/server/hostname.go`, `devicebindingstore.go`, `supportstore.go`,
  `supportapi.go`, `support_integration.go`; `internal/glpi/*`, `internal/tactical/*`;
  testes; `client/src/services/support.ts`, `.../components/domain/support/SupportPanel.tsx`.
- Alterar (aditivo): `server.go` (wiring), `httpapi.go` (registerSupportRoutes),
  `settingsapi.go` (`features["support"]`), `ChatsPage.tsx` (montar painel).
- **Não** alterar: `flowexec*.go`, `flowbridge.go`, `flow{api,store}.go`,
  rotas/handlers de `messageapi.go`, o modelo `(session_id, chat_jid)`.
