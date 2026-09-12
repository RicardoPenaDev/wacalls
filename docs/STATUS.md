# Estado atual

Atualizado em: 2026-09-12
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa atual: `T-005` — EM ANDAMENTO (fundação de persistência com FK composta aprovada em SQLite e MariaDB 11.4).
Push e integrações de runtime ainda não autorizados.

## Objetivo imediato

Revisão final da fundação de persistência auditada e testada antes de iniciar `GetTicket` e o orquestrador/service.

```text
fundação T-005 (SQLite + MariaDB 11.4 100% OK) → revisão final → GetTicket/service → HTTP API/wiring
```

## Concluído

- `T-001` — inventário/baseline do código.
- `T-002` — plano técnico do slice; decisões D-013/D-014.
- `T-B001` — pacote `internal/voip/media` recuperado e versionado.
- `T-003` — normalização de hostname e store `device_bindings`, com SQLite.
- `T-004` — clientes `internal/glpi` e `internal/tactical`, testes offline.
- `chore` — `.gitattributes` multiplataforma com política explícita de EOL.
- `T-B002` — harness MariaDB descartável (`test/mariadb/compose.yml`, `internal/testdb`, `scripts/test-store-contracts.{sh,ps1}`); validado 100% em SQLite e MariaDB 11.4.
- `T-005 (especificação)` — plano de implementação finalizado com D-017 e D-018.
- `T-005 (fundação de persistência — integridade referencial, testes e validação multi-backend)`:
  - `cmd/server/support_types.go` e `support_types_test.go` (tipos, enums, canonical payload v2, external_id, crypto tokens).
  - `cmd/server/supportstore.go`:
    - Foreign key composta formal: `support_request_events (tenant_id, support_request_id) REFERENCES support_requests (tenant_id, id) ON UPDATE RESTRICT ON DELETE RESTRICT`.
    - Restrição de unicidade composta correspondente: `uq_support_requests_tenant_id UNIQUE (tenant_id, id)` em SQLite e MariaDB.
    - Tipos e collations rigorosamente espelhados: `tenant_id` (`VARCHAR(128) COLLATE utf8mb4_bin`), `id`/`support_request_id` (`VARCHAR(36) CHARACTER SET ascii COLLATE ascii_bin`).
    - Ativação obrigatória de `PRAGMA foreign_keys = ON;` na conexão SQLite.
    - CHECK constraint de estados (`processing`, `synced`, `retryable_error`, `unknown`, `failed`) e actor_type (`user`, `system`).
    - Geração de IDs de evento via `uuid.NewV7()` para ordenação cronológica monotônica estrita.
    - Desempate determinístico em `ListEvents` (`ORDER BY created_at ASC, id ASC`) e `ListByConversation` (`ORDER BY created_at DESC, id DESC`).
  - `cmd/server/supportstore_test.go`: 23 testes de contrato cobrindo rejeição de FK inexistente, rejeição de tenant divergente, bloqueio de deleção de pai com eventos, rollbacks atômicos separados em `created` e `ticket_claimed`, concorrência 10x, CAS retry e recovery de órfãos.
  - Ambiente de teste Docker Desktop (WSL2 backend) provisionado no Windows; driver MariaDB ajustado com `clientFoundRows=true`.
  - Validação completa nos dois backends via `scripts/test-store-contracts.ps1`:
    - SQLite: 100% aprovado.
    - MariaDB 11.4 descartável: 100% aprovado.
    - 10 repetições consecutivas no MariaDB 11.4: 10/10 aprovadas.
    - Cleanup pós-teste verificado: zero containers, volumes ou redes remanescentes.

## Ainda falta

- Adicionar `GetTicket` em `internal/glpi/client.go` e DTO `Ticket` em `internal/glpi/types.go`.
- Implementar service/orquestrador de suporte e sanitização HTML individual contra XSS/dupla codificação.
- Implementar handlers HTTP (`supportapi.go`), rotas, disputa 409 no retry e reconciliação segura.
- Wiring no boot (`server.go`) e feature flag `WACALLS_SUPPORT_ENABLED`.

## Bloqueios

- Vínculo nativo Ticket↔Computer: indisponível na API GLPI v2.3; mantido contexto textual.
- `conversation_id`: fora do MVP; bloqueia somente portal futuro.
- Push e etapas posteriores da T-005: bloqueados até autorização explícita.

## Estado Git do checkpoint

- Branch `main` em dia com `origin/main` no início da etapa.
- Commit `feat(support): add support request persistence foundation` emendado localmente.
- Nenhum push realizado ou autorizado.

## Próximo passo

Revisão final da fundação de persistência antes de iniciar a implementação do GLPI `GetTicket` e do service.

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
