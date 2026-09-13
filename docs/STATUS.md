# Estado atual

Atualizado em: 2026-09-13
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa atual: `T-007` — Etapa 7.1 publicada; Etapa 7.2 corrigida localmente após auditoria final (achado de sanitização de URL corrigido), aguardando nova auditoria read-only.

## Checkpoint de Passagem (Transição de IA)

- **T-007 Etapa 7.1:** Concluída e publicada em `origin/main` (teste determinístico de expiração de licença).
- **T-007 Etapa 7.2:** Implementada e corrigida localmente no commit atual:
  - O commit inicial `62f4fce` foi **reprovado na auditoria** devido ao uso indevido de `WACALLS_GLPI_INSECURE_TLS` no código Go e problemas de normalização de portas no origin matching; corrigido via `git commit --amend`.
  - A auditoria final seguinte identificou que `sanitizeGLPIWebUrl` (frontend) não rejeitava hash/fragmento, parâmetros de query adicionais nem `id` duplicado, apesar de a especificação declarar essa proteção como atendida; **corrigido** nesta rodada (ver `client/src/lib/supportUrl.ts`).
  - `WACALLS_GLPI_INSECURE_TLS` foi **removida completamente** do código executável e da documentação técnica.
  - Protocolo HTTP é **rejeitado incondicionalmente** para `WACALLS_GLPI_WEB_BASE_URL`, inclusive em `localhost`, `127.0.0.1` e `[::1]`. **Isso não se aplica a `WACALLS_GLPI_BASE_URL`** (URL base da API GLPI, herdada da T-004), que continua aceitando `http` ou `https` — restrição pré-existente, fora do escopo desta etapa.
  - TLS permanece 100% verificado; sem flag nem opção para `InsecureSkipVerify`.
  - Comparação de origem (`matchGLPIOrigins`) considera esquema, hostname normalizado (com suporte a IPv6 e case-insensitive) e porta efetiva.
  - HTTPS sem porta e HTTPS `:443` são **rigorosamente equivalentes**.
  - Portas não padrão divergentes são estritamente rejeitadas.
  - `WACALLS_GLPI_WEB_BASE_URL` continua **opcional**.
  - Quando ausente, a ação "Abrir no GLPI" fica oculta e o botão "Copiar número" continua disponível.
  - `glpiTicketHref` permanece estritamente interno; o DTO público e o frontend recebem apenas `webUrl` sanitizada.
  - `sanitizeGLPIWebUrl` (frontend) exige protocolo `https:`, ausência de userinfo, pathname exato `/front/ticket.form.php`, hash/fragmento vazio e exatamente um único parâmetro de query chamado `id` com valor decimal positivo canônico; hash, parâmetros adicionais (mesmo vazios) e `id` duplicado são rejeitados, retornando `null` sem lançar exceção.
  - Validações completas aprovadas: `gofmt` limpo, `go vet` limpo, 50 repetições de `Support|GLPI` 100% aprovadas, `npm test` 10/10, `npm run build` aprovado, `go build ./...` e `go test -count=1 ./...` globais aprovados.
  - Working tree estava 100% limpa antes deste checkpoint.
- **T-007 Etapa 7.3:** **NÃO iniciada**.
- **Push:** **Nenhum push da Etapa 7.2 foi realizado**.

## Decisões Arquiteturais

- **D-019:** Avaliação de arquitetura de rate limiting mantida em aberto como **proposta** para a Etapa 7.5 (sem implementação antecipada).
- **D-020:** Hardening do link web do GLPI (Opção A restrita) **aceita** e implementada.

## Bloqueios e Próximos Passos Obrigatórios

1. **Próxima Ação Obrigatória:** Auditoria técnica final **READ-ONLY** do commit corrigido antes de autorizar push.
2. Após aprovação formal e publicação da Etapa 7.2 via push em `origin/main`, planejar e executar a **Etapa 7.3** (suíte E2E permanente).
3. **Instrução para a próxima IA:** Ler obrigatoriamente `AGENTS.md`, `docs/STATUS.md`, `docs/DECISIONS.md` e `docs/tasks/T-007-HOMOLOGACAO-HARDENING-DEPLOY.md` antes de qualquer ação.

## Estado Git

- Baseline: `origin/main` = `559cc53789e4a20a34126b41f545347ff4947c02`
- HEAD: 1 commit à frente de `origin/main` (Etapa 7.2 amend).
- Working tree: Limpa.
- Push: Não realizado.
