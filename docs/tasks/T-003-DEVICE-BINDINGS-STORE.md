# T-003 — device_bindings store + normalização de hostname

Status: pronta (primeira tarefa de implementação). Pequena e verificável,
**offline** (sem GLPI/Tactical, sem WhatsApp, sem client). Escopo desenhado em
`docs/tasks/T-002-PLANO-SLICE-GLPI-TACTICAL.md`.

> **Bloqueio de validação (pré-existente):** `cmd/server` não compila hoje porque
> `internal/voip/call` importa `wacalls/internal/voip/media`, que não existe no
> repositório (ver `docs/STATUS.md` → Baseline). Como `go test ./cmd/server/...`
> compila o pacote inteiro, os testes de T-003 só rodarão após restaurar/adicionar
> `internal/voip/media` ou obter o código completo. Não é parte do escopo de T-003;
> decidir antes de iniciar.

## Resultado esperado

Um store `device_bindings` funcionando com criação/upsert idempotente e busca por
hostname, mais funções puras de normalização e correspondência exata de hostname,
cobertas por testes que rodam com SQLite em memória. Nenhuma rota, nenhum
componente, nenhuma integração externa nesta tarefa.

## Contexto mínimo

- Ler: `AGENTS.md`, `docs/STATUS.md`, `docs/tasks/T-002-*.md`, `docs/HOSTNAMES.md`.
- Padrões de referência (só leitura): `cmd/server/tagstore.go` (convenção de
  store), `cmd/server/db_test.go` / `cmd/server/sessionstore_test.go` (SQLite em
  memória nos testes), `cmd/server/chatmetastore.go` (upsert `ON CONFLICT`).

## Dentro do escopo

- `cmd/server/hostname.go` — funções puras:
  - `normalizeHostname(s string) string` — `TrimSpace` + `ToUpper`; sem inserir
    hífens nem "corrigir" aproximação.
  - `parseHostname(s string) (sector string, ok bool)` — extrai setor do padrão
    `{SEC}-{UNI}-{SETOR}-{NN}`; retorna `ok=false` para legados fora do padrão
    (não inventar setor).
  - `hostnamesMatch(a, b string) bool` — verdadeiro só na igualdade após
    normalização (correspondência **exata**).
- `cmd/server/devicebindingstore.go` — `type deviceBindingStore struct{ db *sql.DB }`
  + `newDeviceBindingStore(ctx, db) (*deviceBindingStore, error)` criando a tabela
  e o índice único `(tenant_id, hostname_normalized)` no boot; métodos:
  - `Upsert(ctx, DeviceBinding) (DeviceBinding, error)` — idempotente via
    `ON CONFLICT(tenant_id, hostname_normalized) DO UPDATE`.
  - `FindByHostname(ctx, tenantID, hostname string) (DeviceBinding, bool, error)`
    — normaliza e busca correspondência exata.
  - `Search(ctx, tenantID, query string) ([]DeviceBinding, error)` — por prefixo
    do hostname normalizado.
  - `Get(ctx, id) (DeviceBinding, error)` + `ErrDeviceBindingNotFound`.
- Schema conforme T-002 (campos `id, owner_id, tenant_id, hostname,
  hostname_normalized, tactical_agent_id, glpi_computer_id, tactical_client_id,
  tactical_site_id, sector_code, patrimonio, match_status, last_verified_at,
  created_at, updated_at`). DDL compatível com SQLite e MariaDB.

## Fora do escopo

- Qualquer rota HTTP, handler ou `registerSupportRoutes` (fica para T-005).
- Clientes GLPI/Tactical (T-004).
- Client/React e feature flag (T-006).
- Alterar `server.go`/`httpapi.go` para expor o store (só instanciar é opcional;
  se instanciar, fazê-lo sem registrar rotas). Preferir não fiar ainda.
- Tocar Flow Builder, `messageapi.go` ou o modelo `(session_id, chat_jid)`.

## Critérios de aceitação

1. `go build ./...` compila.
2. `go test ./cmd/server/ -run 'Hostname|DeviceBinding'` passa.
3. Normalização: `" sde-ars-rcp-02 "` → `SDE-ARS-RCP-02`; `hostnamesMatch`
   verdadeiro para variações só de caixa/espaço e **falso** para
   `SDE-ARS-RCP-02` vs `SDE-ARS-RCP01`.
4. `parseHostname` extrai `RCP` de `SDE-ARS-RCP-02` e retorna `ok=false` para
   `SDE-BEA-COORD`.
5. `Upsert` do mesmo `hostname_normalized` no mesmo tenant **não** cria segunda
   linha (idempotente); tenants diferentes coexistem.
6. Nenhuma rota nova exposta; comportamento do WhatsApp inalterado.

## Plano de validação

- `go test ./cmd/server/ -run 'Hostname|DeviceBinding'` (novos testes).
- `go build ./...`.
- Se `go` estiver indisponível no ambiente, registrar o motivo em
  `docs/STATUS.md` e deixar os testes prontos para execução.

## Riscos

- Diferenças de DDL SQLite × MariaDB: manter tipos `TEXT`/`INTEGER` e
  `CREATE ... IF NOT EXISTS`; evitar sintaxe específica.
- Índice único parcial: MariaDB não suporta índice com `WHERE`; usar índice
  único simples em `(tenant_id, hostname_normalized)`.

## Resultado obtido

- (preencher ao concluir)
