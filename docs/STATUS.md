# Estado atual

Atualizado em: 2026-09-12
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa atual: `T-005` — especificação finalizada e aprovada; implementação não iniciada.
`T-005` permanece não iniciada; implementação e push não autorizados.

## Objetivo imediato

Aguardar autorização explícita para iniciar a implementação do código da `T-005`
(`support_types.go`, `supportstore.go`, DDL SQLite/MariaDB e testes contratuais).

```text
revisão final T-005 concluída → autorização explícita → implementação T-005
```

## Concluído

- `T-001` — inventário/baseline do código.
- `T-002` — plano técnico do slice; decisões D-013/D-014.
- `T-B001` — pacote `internal/voip/media` recuperado e versionado.
- `T-003` — normalização de hostname e store `device_bindings`, com SQLite.
- `T-004` — clientes `internal/glpi` e `internal/tactical`, testes offline.
- `chore` — `.gitattributes` multiplataforma com política explícita de EOL.
- `T-B002` — harness MariaDB descartável (`test/mariadb/compose.yml`, `internal/testdb`, `scripts/test-store-contracts.{sh,ps1}`); validado 100% em SQLite e MariaDB 11.4; cleanups de sucesso e falha comprovados (commit `e4d3966`).
- `T-005 (especificação)` — plano de implementação finalizado, com decisões D-017 e D-018 integradas.

## Ainda falta

- Autorização explícita para iniciar código da T-005.
- Implementar T-005 (store, orquestrador, API e wiring).

## Contratos fechados para T-005

### Criação atômica e eliminação de `new` órfão
- `StateNew` não é estado persistente; `sync_state` grava diretamente `processing`.
- Inserção inicial, `processing_token`, `processing_started_at` e eventos `created` + `ticket_claimed` são comitados na mesma transação atômica.
- Elimina requests presas em `new`; qualquer interrupção de processo é coberta por `RecoverOrphanedProcessing`.
- Enriquecimento de snapshot na Transação 2 condicionado estritamente ao `processing_token`.

### Disputa concorrente no `/retry`
- `POST /api/support/requests/{id}/retry` aceita somente `retryable_error`.
- `unknown`, `processing`, `synced` e `failed` respondem `409 state_conflict`; nenhum segundo POST.
- Vencedor do CAS (`RowsAffected()==1`) executa `CreateTicket` e responde `202`. Perdedor responde `409 state_conflict`.

### Reconciliação segura e validação de `external_id`
- `POST /api/support/requests/{id}/reconcile` com `outcome=synced` exige `currentUser.IsAdmin()`.
- Rota `GET /Assistance/Ticket/{id}` confirmada no OpenAPI GLPI v2.3; adicionado `GetTicket` em `internal/glpi`.
- Backend consulta o GLPI fora de transação, valida que o ticket existe e confirma igualdade estrita de `external_id`.
- Rejeita href do body; deriva ID e href oficiais estritamente da resposta do GLPI.
- Divergência retorna `422 validation_failed` (`reconcile_external_id_mismatch`) e mantém `unknown`.

### Recuperação administrativa de órfão
- `outcome=processing_orphaned` exige `IsAdmin()` e aceita apenas solicitações com `processing_started_at <= cutoff`.
- Rejeita ticket ID; transiciona via CAS para `unknown` e grava `processing_recovered_unknown`. Divergência retorna `409`.

### Sanitização HTML e proteção XSS
- Banco e JSON armazenam texto original puro (sem entidades HTML como `&lt;`). Proibido `dangerouslySetInnerHTML` no frontend.
- `TicketInput.Content` escapa individualmente cada campo do usuário com `html.EscapeString` antes de converter `\n` para `<br>`.

### Instâncias, timeouts e rate limit
- Single-instance obrigatório no MVP (D-017); horários em UTC.
- `createTimeout`: default 120s (30–600s); `recoveryMargin`: default 30s (5–300s); `orphanAge=createTimeout+recoveryMargin`.
- Rate limiting in-memory por tenant postergado para hardening futuro (removido da T-005); 429 do GLPI vira `retryable_error`.

### Dialeto e integridade de produção
- `SQLDialect` explícito (`DialectSQLite` em runtime de produção).
- Proibição estrita: código de produção (`cmd/server`) NUNCA deve importar `internal/testdb`.

### Vínculo de equipamento durante processamento
- `PUT /device` durante `processing` responde `200 OK` com `glpiContextUpdated: false`, altera apenas seleção local e não toca no snapshot `ticket_*`.

## Bloqueios

- Vínculo nativo Ticket↔Computer: indisponível na API GLPI v2.3; T-005 mantém contexto textual.
- `conversation_id`: fora do MVP; bloqueia somente o portal futuro.
- Implementação T-005: bloqueada até autorização formal.

## Estado Git do checkpoint

- Branch `main` em dia com `origin/main`.
- Documentação revisada pronta para commit local.
- Nenhum push realizado ou autorizado.

## Próximo passo

Aguardar autorização formal para iniciar a implementação da `T-005` (`support_types.go`, `supportstore.go` e testes de contrato SQLite/MariaDB).

## Ambiente preservado

- Windows 11 x64.
- Go 1.26.4 portátil fora do repositório; caches em `D:/fabrica/WaCalls/toolchains/`.
- Nenhuma credencial real no repositório.

## Não tocar nesta etapa

- `client/`, portal/agente Windows e Flow Builder;
- rotas existentes de `messageapi.go` e modelo `(session_id, chat_jid)`;
- automação remota Tactical, retry automático ou worker GLPI;
- chamadas externas reais ou código runtime até autorização.
