# Estado atual

Atualizado em: 2026-09-12
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa atual: `T-005` — EM ANDAMENTO (GLPI GetTicket + SupportService concluídos; pendente HTTP API e wiring).
Push e integrações de runtime ainda não autorizados.

## Objetivo imediato

Implementação da camada HTTP API (`supportapi.go`), rotas de suporte e wiring no boot (`server.go`).

```text
fundação T-005 (OK) → GetTicket/SupportService (OK) → HTTP API/rotas → wiring/feature flag
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
  - `internal/glpi`: `GetTicket` em `client.go` e `Ticket` em `types.go`, validando IDs numéricos positivos, rota `GET /api.php/v2.3/Assistance/Ticket/{id}`, renovação de token OAuth 401, limites de body e redação de credenciais/URLs.
  - `cmd/server/support_content.go`: formatação de conteúdo de tickets com sanitização HTML (`html.EscapeString` por fragmento de entrada do usuário/dispositivo) e conversão de quebras de linha para `<br>`, prevenindo XSS e dupla codificação.
  - `cmd/server/support_integration.go`: interfaces de consumidores (`supportGLPIClient`, `supportTacticalClient`, `supportStoreBackend`, `deviceBindingStoreBackend`).
  - `cmd/server/supportservice.go`:
    - Criação atômica e detecção de replays simultâneos via token de posse.
    - Resolução de equipamento e enriquecimento de snapshot condicionado ao `processing_token`.
    - Chamadas externas `CreateTicket` e `GetTicket` estritamente fora de transações de banco de dados.
    - Finalização CAS no banco com classificação fechada (`synced`, `failed`, `retryable_error`, `unknown`).
    - Disputa de retry com CAS e proteção contra requisições concorrentes.
    - Reconciliação administrativa com validação obrigatória do `external_id` remoto contra o registro local.
    - Atualização de equipamento local preservando snapshot congelado da tentativa (`glpiContextUpdated: false`).
    - Métodos de conveniência `GetByID` e `ListByConversation` com garantia de tenant isolation.
  - Testes unitários 100% offline aprovados:
    - 28 testes em `internal/glpi` (6 específicos de `GetTicket`).
    - 4 testes em `cmd/server/support_content_test.go`.
    - 8 suítes com múltiplos subtestes em `cmd/server/supportservice_test.go` cobrindo criação, concorrência, ausência de transações abertas na chamada HTTP, classificação completa de falhas, disputa de retry, reconciliação segura e isolamento multi-tenant.

## Ainda falta

- Implementar handlers HTTP (`supportapi.go`), rotas, validações de requisição e reconciliação administrativa.
- Wiring no boot (`server.go`) com verificação de instância única, cutoff de órfãos e feature flag `WACALLS_SUPPORT_ENABLED`.

## Bloqueios

- Vínculo nativo Ticket↔Computer: indisponível na API GLPI v2.3; mantido contexto textual.
- `conversation_id`: fora do MVP; bloqueia somente portal futuro.
- Push e etapas posteriores da T-005: bloqueados até autorização explícita.

## Estado Git do checkpoint

- Branch `main` em dia com `origin/main` no início da etapa (`d0f62b9254b0603f4a6131ded1dbd4be3a1f556b`).
- Nova etapa implementada localmente em arquivos isolados.
- Nenhum push realizado ou autorizado.

## Próximo passo

Implementação dos handlers HTTP (`supportapi.go`) e rotas correspondentes conforme especificação T-005.

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
