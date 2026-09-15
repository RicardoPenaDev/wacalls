# Estado atual

Atualizado em: 2026-09-15
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa atual: `T-007` — Etapas 7.1–7.3 concluídas e publicadas. Etapa 7.4
(Homologação Real Controlada): Gates 1–4 executados; GLPI aprovado (ticket
#4, aberto, intocado). Tactical ainda não aprovado após duas correções
publicadas (7.4-R1 `a014e58`, 7.4-R2 `162e8cf`) e uma terceira, 7.4-R3,
implementada agora com a causa raiz comprovada e corrigida (`local_ips`:
string vs. `[]string`) — commit local, **não publicada**. Gate 5 não
iniciado.

## Status das Etapas da T-007

- **7.1–7.2:** Concluídas e publicadas em `origin/main`.
- **7.3 (Suíte E2E Permanente):** Concluída, publicada e **validada no GitHub
  Actions**. Commit `1696cfb5f7b766a17d1cc864bd21db83cc214d28`. Mitigações de
  travamento de CI, timeouts escalonados e teste de teardown descritos em
  `docs/tasks/T-007-HOMOLOGACAO-HARDENING-DEPLOY.md`. Matriz E2E 12/12 (ver
  seção abaixo).
- **7.4 (Homologação Real Controlada):** ambiente pessoal de Ricardo, dados
  sintéticos, ticket `[HOMOLOGAÇÃO T-007] Validação controlada RicardoSMS`.
  - **Gates 1–3:** Aprovados (preflight read-only; pareamento WhatsApp;
    mensagem controlada com ressalva D-2B-01; integrações read-only).
  - **Gate 4 GLPI:** Aprovado — ticket real **#4** criado (`external_id`
    canônico, `glpi_computer_id=59` confirmado em `device_bindings` e no
    texto do ticket). Permanece aberto; **nunca alterado desde a criação**,
    em nenhuma das tentativas abaixo.
  - **Gate 4 Tactical — histórico de tentativas (ainda não aprovado):**
    1. **Tentativa original:** `missing_tactical`. Causa comprovada por
       código: `SupportService` descartava o `tactical.Agent` retornado
       por `FindAgentByHostname` sem persistir `tactical_agent_id`.
    2. **7.4-R1** (publicado — commit `a014e58dedcd91acb7f509a99ea67a726342820b`,
       `origin/main`, CI "E2E Support Suite" verde): corrige o descarte
       acima; hardening de `parseLastSeen` para formato legado do Tactical
       (D-022, causa daquele formato nunca confirmada). Refresh seguinte:
       ainda `missing_tactical`, **zero log** explicando por quê —
       `RefreshDeviceBinding` descartava qualquer erro do Tactical sem
       registrar nada.
    3. **7.4-R2** (publicado — commit `162e8cfc55df4ee7ba711e4459b4a9ef4c02fb7c`,
       `origin/main`, CI verde; D-023): observabilidade sanitizada —
       categorias tipadas (`ErrTimeout`/`ErrCanceled` novos), log por
       categoria + status HTTP, hostname só como hash, not-found (`INFO`)
       diferenciado de falha real (`WARN`). Refresh seguinte revelou
       `category=parse_error`, mas o erro Go original ainda era descartado
       ao virar `ErrBadResponse`, sem detalhe estrutural.
    4. **Diagnóstico offline (2026-09-15, sem alterar código):**
       reprodução local do decode contra o corpo real capturado confirmou
       `*json.UnmarshalTypeError` em `local_ips`: a API do Tactical retorna
       `local_ips` como **string** (endereço único, ou vários separados
       por vírgula + espaço — confirmado em 29/29 agentes reais: 26
       únicos, 3 com vírgula, 0 arrays, 0 outros formatos) enquanto
       `listAgentDTO.LocalIPs` esperava `[]string`. `json.Unmarshal`
       abortava o array inteiro — **qualquer** hostname do tenant ficaria
       `missing_tactical`, não só RicardoSMS; não é falha de rede
       transitória, é 100% reprodutível enquanto a API responder nesse
       formato.
    5. **7.4-R3** (implementada, commit local, **NÃO publicada**; refresh
       real **NÃO repetido**): corrige a causa raiz — ver **D-024**.
       `flexibleLocalIPs` aceita string única, string separada por vírgula
       (com `TrimSpace`), array de strings, `null` e string vazia; um tipo
       verdadeiramente incompatível (number/boolean/object/elemento não-
       string em array) marca só aquele campo como inválido
       (`LocalIPsValid=false`) sem abortar o agente nem os demais. Erros de
       decode remanescentes (`*json.UnmarshalTypeError`/`*json.SyntaxError`
       em qualquer outro campo) agora preservam campo/tipo Go/tipo
       JSON/offset sanitizados em vez de serem descartados por completo.
       Testes cobrem os 12 formatos pedidos (IPv4/IPv6/espaços/vazio/null/
       array único/array múltiplo/vírgula/number/boolean/object/elemento
       inválido em array) mais um teste de ponta a ponta com
       `tactical.Client` real (não mock) contra fixture com o schema real
       (dados sintéticos, RFC 5737), provando `missing_tactical → matched`
       com `glpi_computer_id=59` preservado. Validação: `gofmt`,
       `go vet ./...`, `go build ./...`, `go test -count=1 ./...` (100%
       verde), `npm test` (21/21), `npm run build`, `npm run test:e2e`
       (12/12), `git diff --check` — todos aprovados.
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
- **D-023:** Observabilidade sanitizada de erros do Tactical (T-007 7.4-R2):
  categorias tipadas (`ErrTimeout`/`ErrCanceled` novos) e log sanitizado
  (categoria + status HTTP, hostname só como hash) para toda falha de
  `FindAgentByHostname`, antes descartada sem log. Detalhe completo em
  `docs/DECISIONS.md`.
- **D-024:** Parser tolerante para `local_ips` do Tactical (T-007 7.4-R3):
  aceita string única, string separada por vírgula, array de strings, null
  e string vazia; tipo verdadeiramente incompatível marca só o campo como
  inválido, nunca aborta o agente. Causa raiz do `missing_tactical`
  observado em duas tentativas reais de refresh. Detalhe completo em
  `docs/DECISIONS.md`.

## Bloqueios e Próximos Passos Obrigatórios

1. **Próxima Ação:** Autorização para push do commit local de correção
   7.4-R3 (parser tolerante de `local_ips` + preservação sanitizada de
   erro de decode).
2. Após o push, autorizar **uma** nova chamada controlada a
   `POST /api/support/devices/{id}/refresh` no binding `RicardoSMS`
   existente (ticket #4 intocado, sem chamado novo). Com a causa raiz
   corrigida, o resultado esperado é `match_status=matched`; se ainda
   falhar, o log sanitizado (7.4-R2) identifica categoria e status HTTP,
   e agora também campo/tipo Go/tipo JSON/offset quando for outro
   problema de decode (7.4-R3).
3. Confirmar timezone real do formato legado do Tactical caso ele volte a
   ocorrer, antes de tratá-lo como confiável.
4. Após confirmação da Tactical em homologação real, avançar para o Gate 5.
5. **Instrução para a próxima IA:** Ler `AGENTS.md`, `docs/STATUS.md`,
   `docs/DECISIONS.md` e `docs/tasks/T-007-HOMOLOGACAO-HARDENING-DEPLOY.md`.

## Estado Git

- Branch: `main`.
- `origin/main` == `HEAD` em `162e8cfc55df4ee7ba711e4459b4a9ef4c02fb7c`
  (7.4-R2, publicado, CI "E2E Support Suite" verde).
- Commit local pendente de push: correção 7.4-R3 (`flexibleLocalIPs`
  tolerante a string/array/vírgula/null; preservação sanitizada de
  `*json.UnmarshalTypeError`/`*json.SyntaxError` em `tactical.Error`).
- Push: Não realizado.
