# Estado atual

Atualizado em: 2026-09-14
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa atual: `T-007` — Etapas 7.1 e 7.2 concluídas e publicadas; Etapa 7.3 com mitigação preventiva de travamentos no CI e hardening de execução E2E concluídos.

## Status das Etapas da T-007

- **T-007 Etapa 7.1:** Concluída e publicada em `origin/main`.
- **T-007 Etapa 7.2:** Concluída e publicada em `origin/main` (D-020).
- **T-007 Etapa 7.3:** Correções e mitigação preventiva de travamentos implementadas no commit local:
  - **Mitigação da Hipótese A (Fechamento do Sheet no Teste 11):** Substituição de `Escape` por clique restrito e determinístico no botão visual de fechar (`getByRole("dialog").getByRole("button", { name: "Fechar", exact: true })`); assertions atômicas com timeout de 5s para confirmação de painel oculto, conversa inalterada e rascunho preservado; sem tocar em código de produção.
  - **Mitigação da Hipótese B (Conexões de Controle nos Mocks):** Hardening em `fixtures.ts` com `agent: false`, `Connection: close`, timeout estrito de 5s e descarte imediato (`req.destroy`); drenagem via `req.resume()` nas rotas de controle dos mocks sem consumo de payload.
  - **Mitigação da Hipótese C (Timeout de Fase e Árvore de Processos):** Runner (`run.mjs`) com timeout de fase de 5 min (300s) e encerramento de toda a árvore de processos descendentes via `killProcessTree` (`taskkill /T /F` no Windows, `process.kill(-pid)` no Unix); exit code != 0 propagado em falhas; `finally` garantindo `tracker.teardown()`.
  - **Mitigação da Hipótese D (Observabilidade de Transição entre Fases):** Logs marcadores registrando início e fim de cada fase, teardown de cada PID e início da fase sem Tactical.
  - **Escalonamento Realista de Timeouts:**
    - Requisições mock: 5s;
    - Testes rápidos: 45s (`playwright.config.ts`);
    - Teste de Retry após 429: timeout específico de 120s (`test.setTimeout(120_000)` restrito a este teste);
    - GlobalTimeout Playwright por fase: 240s (4 min);
    - Runner timeout por fase: 300s (5 min);
    - Step E2E no GitHub Actions: 15 min (`.github/workflows/e2e.yml`);
    - Job GitHub Actions: 20 min.
  - **Teste Automatizado de Timeout do Runner:** Script `client/tests/e2e/runner-timeout.test.mjs` validando encerramento de processos pais e descendentes, execução do teardown, retorno de erro e ausência de resíduos temporários.
  - **Validações Aprovadas:**
    - Teste do Caso 11 executado com sucesso isolado;
    - 1ª execução completa da suíte E2E aprovada (12/12);
    - Teste automatizado de timeout/cleanup aprovado;
    - Duas execuções completas consecutivas da suíte E2E aprovadas (12/12 em ambas);
    - `npm test` 21/21 aprovados;
    - `npm run build` aprovado;
    - `gofmt -l`, `go vet ./...`, `go build ./...`, `go test -count=1 ./...` 100% verdes;
    - `git diff --check` limpo.
- **T-007 Etapa 7.4:** **NÃO iniciada**.
- **Push:** **NÃO realizado**.

## Matriz E2E (12/12, todos com assertion real no navegador)

1. Equipamento vinculado: hostname + badge "Vinculado" (`support-workflow.e2e.spec.ts`).
2. `processing → synced`: mock retido, refresh manual observa "Sincronizando" real, data de criação no período atual (sem 1970).
3. Rascunho do composer + conversa preservados ao abrir e fechar o painel de suporte.
4. `support=true, tactical=false`: boot real dedicado sem Tactical (`support-tactical-disabled.e2e.spec.ts`).
5. Operador sem reconciliação em `unknown`.
6. Admin reconciliando `unknown`.
7. Retry após 429: bloqueado até prazo real (60s), liberado, sincronizado.
8. Duplo clique: exatamente 1 POST, 1 Idempotency-Key.
9. `webUrl` segura + target/rel + "Copiar número" com clipboard real.
10. Polling encerrado ao fechar o painel.
11. Polling encerrado ao trocar de conversa.
12. Network Guard: auto-testes bloqueando HTTP/HTTPS externo, WebSocket externo e permitindo loopback.

## Decisões Arquiteturais

- **D-019:** Rate limiting — proposta em aberto para a Etapa 7.5.
- **D-020:** Hardening do link web do GLPI (Opção A restrita) — aceita.
- **D-021:** Modo E2E restrito (`-e2e-mode`), sessão sintética em memória, validação estrutural de `runDir`/DB temporário, CA privada restrita a loopback e validação única via `precheckE2EBoot`.

## Bloqueios e Próximos Passos Obrigatórios

1. **Próxima Ação:** Conclusão da Etapa 7.3 no commit amendado local e aguardo de autorização para push.
2. Após push, planejamento e autorização da Etapa 7.4 (Homologação Real Controlada).
3. **Instrução para a próxima IA:** Ler `AGENTS.md`, `docs/STATUS.md`, `docs/DECISIONS.md` e `docs/tasks/T-007-HOMOLOGACAO-HARDENING-DEPLOY.md`.

## Estado Git

- Branch: `main`
- Baseline: `origin/main` = `057f0dd`
- Commit único local (amendado): `test(e2e): add permanent support workflow coverage`
- Push: Não realizado.
