# Estado atual

Atualizado em: 2026-09-12
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa atual: `T-B002` — concluída. T-005 pronta para revisão final de contrato (implementação não iniciada).
`T-005` permanece não iniciada; implementação e push não autorizados.

## Objetivo imediato

Fazer a revisão final da especificação `docs/tasks/T-005-SUPPORT-REQUESTS-API-WIRING.md`
e aguardar autorização antes de iniciar qualquer código runtime ou push.

```text
revisão final T-005 → autorização explícita → implementação T-005
```

## Concluído

- `T-001` — inventário/baseline do código.
- `T-002` — plano técnico do slice; decisões D-013/D-014.
- `T-B001` — pacote `internal/voip/media` recuperado e versionado.
- `T-003` — normalização de hostname e store `device_bindings`, com SQLite.
- `T-004` — clientes `internal/glpi` e `internal/tactical`, testes offline.
- `chore` — `.gitattributes` multiplataforma com política explícita de EOL.
- `T-B002` — harness MariaDB descartável (`test/mariadb/compose.yml`, `internal/testdb`, `scripts/test-store-contracts.{sh,ps1}`); validado 100% em SQLite (PowerShell Windows e Bash macOS) e MariaDB 11.4 (Bash macOS); cleanups de sucesso e falha comprovados.

## Ainda falta

- Fazer a revisão final do contrato T-005 após o aceite da T-B002.
- Implementar T-005 somente após autorização explícita.

## Contratos fechados para T-005

### Retry formal

- `POST /api/support/requests/{id}/retry` aceita somente `retryable_error`.
- Atendente comum pode executar quando tenant e conversa forem acessíveis.
- `new`, `processing`, `unknown`, `synced` e `failed` retornam `409`; não há POST
  GLPI nesses estados.
- Cada retry gera novo `processing_token` e disputa CAS; somente
  `RowsAffected()==1` chama GLPI.
- `Idempotency-Key`, `external_id` e fingerprint são preservados.
- Sucesso de aceite responde `202`. Não existe retry automático; `unknown`
  requer reconciliação.

### Permissões

- `user_permissions` persiste strings e `currentUser.Permissions` é `[]string`.
- O backend não tem enforcement granular/`HasPermission`, e o catálogo frontend
  não contém `support.reconcile`.
- Logo, reconcile e recovery administrativo exigem `currentUser.IsAdmin()` no
  MVP. `support.reconcile` fica para evolução futura completa de backend + UI.

### Instâncias e recovery

- A implantação atual tem um container WACalls, SQLite local/pool de uma conexão
  e estado de broker/sessões/rate limit em memória; não há lease ou coordenação.
- MVP exige uma única instância ativa por implantação (D-017).
- O claim grava `processing_started_at` em UTC.
- `createTimeout`: default 120 s, mínimo 30, máximo 600.
- `recoveryMargin`: default 30 s, mínimo 5, máximo 300.
- `orphanAge=createTimeout+recoveryMargin`;
  `cutoff=time.Now().UTC().Add(-orphanAge).Unix()`.
- Startup, após schema e antes das rotas de suporte, recupera somente
  `processing_started_at<=cutoff` por CAS. Leitura nunca altera estado.
- Sem heartbeat. A rota administrativa reutiliza a rotina e exige `IsAdmin()`.

## Bloqueios

- Vínculo nativo Ticket↔Computer: indisponível na API GLPI v2.3; T-005 mantém
  contexto textual.
- `conversation_id`: fora do MVP; bloqueia somente o portal futuro.
- T-005: implementação bloqueada até autorização explícita.

## Escopo da T-B002

- MariaDB 11.4 descartável, isolado, credenciais efêmeras e porta loopback.
- Mesma suíte de contrato SQLite/MariaDB: schema, transação, unique, conflito,
  CAS/`RowsAffected`, concorrência, rollback e cleanup.
- Scripts local/CI com readiness e timeouts; nenhum GLPI/Tactical real.
- Não implementar `support_requests` na T-B002.

## Validação registrada

- T-B002: `gofmt -l` limpo; `go vet ./internal/testdb/...` → **OK**;
  `bash scripts/test-store-contracts.sh all` (macOS) → **100% PASS** (SQLite 0.30s, MariaDB 0.25s);
  `pwsh -NoProfile -File .\scripts\test-store-contracts.ps1 -Backend sqlite` (Windows) → **100% PASS** (SQLite 0.64s; MariaDB não executado no Windows por Docker indisponível, já validado no macOS);
  Cleanup após sucesso comprovado (`docker ps/volume/network ls` limpos no Mac);
  Cleanup após falha comprovado via `WACALLS_TEST_SIMULATE_FAILURE=true` (exit code 1, zero recursos órfãos no Mac);
  `go test ./internal/testdb/... -count=10` → **OK**;
  `go build ./...` e `go test ./...` → **100% PASS**;
  `git diff --check origin/main..HEAD` → **OK**.
- Race detector não executado: CGO desabilitado e `gcc` ausente.

## Estado Git do checkpoint

- Branch `main` à frente de `origin/main` pelo commit da T-B002 (aguardando push).
- Nenhum merge ou rebase pendente.
- Nenhum push realizado.

## Próximo passo

Revisão final da especificação T-005 (`docs/tasks/T-005-SUPPORT-REQUESTS-API-WIRING.md`) e aguardar autorização antes de qualquer código T-005 ou push.

## Ambiente preservado

- Windows 11 x64.
- Go 1.26.4 portátil fora do repositório; caches em
  `D:/fabrica/WaCalls/toolchains/`.
- Nenhuma credencial real no repositório.

## Não tocar nesta etapa

- `client/`, portal/agente Windows e Flow Builder;
- rotas existentes de `messageapi.go` e modelo `(session_id, chat_jid)`;
- automação remota Tactical, retry automático ou worker GLPI;
- API legada GLPI, chamadas externas reais ou vínculo nativo Ticket↔Computer.
