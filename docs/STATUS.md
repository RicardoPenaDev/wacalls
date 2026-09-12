# Estado atual

Atualizado em: 2026-09-12
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa atual: `T-B002` — especificada; implementação não iniciada.
`T-005` permanece bloqueada por T-B002. Implementação e push não autorizados.

## Objetivo imediato

Implementar e validar a infraestrutura de teste descrita em
`docs/tasks/T-B002-HARNESS-MARIADB-STORES.md`. Depois fazer a revisão final do
contrato T-005 e somente então iniciar sua implementação.

```text
T-B002 → revisão final T-005 → implementação T-005
```

## Concluído

- `T-001` — inventário/baseline do código.
- `T-002` — plano técnico do slice; decisões D-013/D-014.
- `T-B001` — pacote `internal/voip/media` recuperado e versionado.
- `T-003` — normalização de hostname e store `device_bindings`, com SQLite.
- `T-004` — clientes `internal/glpi` e `internal/tactical`, testes offline,
  validação de path, limites, redirects e concorrência OAuth.
- Correção documental T-005: contrato de retry, autorização, recuperação de
  órfão, dependência T-B002 e decisão D-017 registrados. Nenhum código runtime,
  SQL, frontend, Docker ou CI foi implementado nesta correção.

## Ainda falta

- Implementar e validar T-B002; nenhum artefato executável do harness existe.
- Fazer a revisão final do contrato T-005 após o aceite local/CI da T-B002.
- Implementar T-005 somente após essa revisão e autorização explícita.

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

- T-005: bloqueada por ausência de harness MariaDB automatizado.
- Vínculo nativo Ticket↔Computer: indisponível na API GLPI v2.3; T-005 mantém
  contexto textual.
- `conversation_id`: fora do MVP; bloqueia somente o portal futuro.

## Escopo da T-B002

- MariaDB 11.4 descartável, isolado, credenciais efêmeras e porta loopback.
- Mesma suíte de contrato SQLite/MariaDB: schema, transação, unique, conflito,
  CAS/`RowsAffected`, concorrência, rollback e cleanup.
- Scripts local/CI com readiness e timeouts; nenhum GLPI/Tactical real.
- Não implementar `support_requests` na T-B002.

## Validação registrada

- Checkpoint documental: `git diff --check` → **OK**.
- Última validação de código, na T-004: `gofmt -l` sem saída;
  `go vet ./internal/glpi/... ./internal/tactical/...` → **OK**;
  testes dos dois pacotes com `-count=20` → **OK**;
  `go build ./...` e `go test ./...` → **OK**.
- Race detector não executado: CGO desabilitado e `gcc` ausente.
- Nenhum código executável mudou desde essa validação; testes Go/frontend não
  se aplicam ao checkpoint documental.

## Estado Git do checkpoint

- Branch `main` acompanha `origin/main` com um commit documental local ainda não
  publicado.
- Não há merge ou rebase pendente.
- Working tree limpa após o amend; nenhum arquivo não commitado.
- Nenhum push realizado.

## Próximo passo

Implementar T-B002 exatamente pela task. Após aceite local e CI, revisar o
contrato T-005, então pedir autorização separada antes de qualquer código T-005
ou push.

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
