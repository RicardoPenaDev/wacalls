# Estado atual

Atualizado em: 2026-09-15
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa atual: `T-007` — Etapas 7.1–7.3 concluídas e publicadas. Etapa 7.4
(Homologação Real Controlada) em andamento: Gates 1–4 executados, correção
7.4-R1 (tactical_agent_id não persistido + hardening de parsing do
Tactical) implementada e validada. Gate 5 não iniciado.

## Status das Etapas da T-007

- **7.1–7.2:** Concluídas e publicadas em `origin/main`.
- **7.3 (Suíte E2E Permanente):** Concluída, publicada e **validada no GitHub
  Actions**. Commit `1696cfb5f7b766a17d1cc864bd21db83cc214d28`. Mitigações de
  travamento de CI, timeouts escalonados e teste de teardown descritos em
  `docs/tasks/T-007-HOMOLOGACAO-HARDENING-DEPLOY.md`. Matriz E2E 12/12 (ver
  seção abaixo).
- **7.4 (Homologação Real Controlada):** Em andamento no ambiente pessoal de
  Ricardo, dados sintéticos, chamado `[HOMOLOGAÇÃO T-007] Validação
  controlada RicardoSMS`.
  - **Gate 1 (Preflight read-only):** Aprovado. GLPI e Tactical de
    laboratório autenticados; ativo `RicardoSMS` localizado em ambos.
  - **Gate 2A (Pareamento WhatsApp):** Aprovado.
  - **Gate 2B (Mensagem controlada):** Aprovado **com ressalva D-2B-01**
    (uma mensagem manual adicional enviada pelo próprio Ricardo; não foi
    duplicidade do sistema; zero chamados GLPI nesta fase).
  - **Gate 3 (Integrações read-only):** Aprovado no escopo read-only.
  - **Gate 4 (Criação do chamado):** **GLPI aprovado** — ticket real **#4**
    criado (`external_id` formato canônico `wacalls-` + 32 hex; 1 POST
    local, 1 POST externo, sem retry; `attempt_count=1`; estado `synced`;
    `glpi_computer_id=59` confirmado em `device_bindings`,
    `support_requests` e no texto do próprio ticket via `run-gate4.ps1`).
    O ticket #4 permanece aberto e não deve ser alterado/fechado.
    **Tactical reprovado**: `device_bindings` ficou com
    `match_status=missing_tactical` e `tactical_agent_id` vazio.
  - **Causa comprovada (código, sem ambiguidade):** `SupportService`
    descartava o `tactical.Agent` retornado por `FindAgentByHostname`
    (`_, tacErr := ...`) antes de persistir `tactical_agent_id` — mesmo
    num lookup bem-sucedido o campo nunca seria preenchido.
  - **Causa de `match_status=missing_tactical` em si:** exige que
    `FindAgentByHostname` tenha retornado erro nessa chamada (confirmado
    pela lógica determinística do switch e pelo estado do banco). **Qual
    erro exato — não comprovado.** `server.stdout.log`/`server.stderr.log`
    não mencionam Tactical/last_seen/RicardoSMS nesse período e o pacote
    `tactical` não tinha logger antes desta correção. A alegação original
    de que a API retornou `last_seen` no formato `MM/DD/YYYY HH:mm:ss`
    (ex. `09/14/2026 22:58:35`) **não tem evidência bruta sobrevivente**.
    Duas consultas reais ao Tactical em 2026-09-15 mostraram 100% RFC3339
    com `Z` em 29/29 agentes, incluindo `RicardoSMS`. Uma reprodução
    controlada mostrou que converter esse valor real para `[datetime]` no
    PowerShell e exibi-lo com `ToString()` padrão nesta máquina produz
    exatamente uma string `MM/dd/yyyy HH:mm:ss` em horário local — a mesma
    forma relatada antes — consistente com artefato de apresentação do
    PowerShell, não com conteúdo bruto da API, mas isso **não pode ser
    confirmado retroativamente com certeza absoluta**.
  - **Correção 7.4-R1 (implementada, commit local pendente de push):**
    - **Causa comprovada, corrigida:** `SupportService` agora captura e
      persiste `tactical_agent_id` a partir do `Agent` de
      `FindAgentByHostname`.
    - **Hardening preventivo** (defeito real, confirmado por inspeção de
      código e testável isoladamente; ligação com o incidente específico
      do Gate 4 permanece não comprovada — ver acima e D-022):
      `parseLastSeen` aceita RFC3339 (com/sem offset/frações — já
      funcionava), vazio/null e espaços sem nunca falhar; formato legado
      é reconhecido mas não confiado (`LastSeenValid=false`, hora zero);
      `ListAgents` não aborta mais a lista inteira por um agente
      malformado (esse defeito, se tivesse ocorrido no Gate 4, derrubaria
      `FindAgentByHostname` para **qualquer** hostname do tenant).
    - Aviso sanitizado (sem valor bruto) quando `LastSeenValid=false`.
    - Novo endpoint administrativo `POST /api/support/devices/{id}/refresh`
      (somente admin, tenant-scoped, somente leituras remotas, idempotente,
      nunca cria segundo binding, preserva IDs válidos em falha parcial,
      usa exclusivamente o hostname já persistido) para corrigir bindings
      existentes sem SQL direto e sem novo ticket.
    - Testes novos/reescritos em `internal/tactical/client_test.go`,
      `cmd/server/supportservice_test.go` e `cmd/server/supportapi_test.go`.
    - Validação: `gofmt`, `go vet ./...`, `go build ./...`,
      `go test -count=1 ./...` (100% verde), `npm test` (21/21),
      `npm run build`, `npm run test:e2e` (12/12) — todos aprovados.
  - **Gate 5:** **NÃO iniciado.**

## Matriz E2E (12/12, todos com assertion real no navegador)

1. Equipamento vinculado: hostname + badge "Vinculado".
2. `processing → synced`: refresh manual observa "Sincronizando" real.
3. Rascunho do composer + conversa preservados ao abrir/fechar o painel.
4. `support=true, tactical=false`: boot real dedicado sem Tactical.
5. Operador sem reconciliação em `unknown`.
6. Admin reconciliando `unknown`.
7. Retry após 429: bloqueado até prazo real (60s), liberado, sincronizado.
8. Duplo clique: exatamente 1 POST, 1 Idempotency-Key.
9. `webUrl` segura + target/rel + "Copiar número" com clipboard real.
10. Polling encerrado ao fechar o painel.
11. Polling encerrado ao trocar de conversa.
12. Network Guard: bloqueia HTTP/HTTPS/WebSocket externo, permite loopback.

## Decisões Arquiteturais

- **D-019:** Rate limiting — proposta em aberto para a Etapa 7.5.
- **D-020:** Hardening do link web do GLPI (Opção A restrita) — aceita.
- **D-021:** Modo E2E restrito (`-e2e-mode`), sessão sintética em memória,
  validação estrutural de `runDir`/DB temporário, CA privada restrita a
  loopback e validação única via `precheckE2EBoot`.
- **D-2B-01:** Ressalva do Gate 2B — mensagem manual adicional de Ricardo
  durante o teste controlado; não foi duplicidade do sistema.
- **D-022:** `parseLastSeen` do Tactical trata formato legado sem offset
  (`MM/DD/YYYY HH:mm:ss`) como não confiável em vez de assumir UTC/local.
  Fuso inconclusivo em 2026-09-15; a própria ocorrência desse formato como
  conteúdo bruto da API durante o Gate 4 também não tem evidência
  sobrevivente (ver Gate 4 acima e `internal/tactical/client.go`).

## Bloqueios e Próximos Passos Obrigatórios

1. **Próxima Ação:** Autorização para push do commit de correção 7.4-R1.
2. Retomar a homologação usando o ticket **#4** existente e o binding
   `RicardoSMS` já criado: chamar `POST /api/support/devices/{id}/refresh`
   (admin) para re-resolver o Tactical e confirmar `match_status=matched` e
   `tactical_agent_id` preenchido — **sem** criar chamado novo.
3. Confirmar timezone real do formato legado do Tactical caso ele volte a
   ocorrer, antes de tratá-lo como confiável.
4. Após confirmação da 7.4-R1 em homologação real, avançar para o Gate 5.
5. **Instrução para a próxima IA:** Ler `AGENTS.md`, `docs/STATUS.md`,
   `docs/DECISIONS.md` e `docs/tasks/T-007-HOMOLOGACAO-HARDENING-DEPLOY.md`.

## Estado Git

- Branch: `main`.
- `origin/main` inclui `1696cfb` (7.3, publicado) mais dois commits
  paralelos não relacionados a T-007 (`1b89333`, `7b07f65` — filtros de
  conexão e alertas Evolution API); sem sobreposição de arquivos, já
  sincronizados via fast-forward.
- Commit local pendente de push: correção 7.4-R1 (tactical_agent_id não
  persistido, hardening preventivo de parsing do Tactical, endpoint de
  refresh administrativo).
- Push: Não realizado.
