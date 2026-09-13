# Estado atual

Atualizado em: 2026-09-13
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa atual: `T-007` — Planejamento de homologação, hardening e deploy concluído (execução não iniciada).

## Objetivo imediato

Aguardar autorização formal do usuário para iniciar a execução da Etapa 7.1 da T-007 (diagnóstico e resolução do teste de licença `TestLicenseStatusFaixasDeVencimento`).

```text
T-005 backend (publicado) → T-006 frontend (publicado) → T-007 planejada (execução não iniciada)
```

## Concluído

- `T-001` — Inventário e baseline do código.
- `T-002` — Plano técnico do slice (decisões D-013/D-014).
- `T-B001` — Recuperação do pacote `internal/voip/media`.
- `T-003` — Normalização de hostnames e store `device_bindings` (SQLite/MariaDB).
- `T-004` — Clientes HTTP `internal/glpi` e `internal/tactical` com testes offline.
- `T-B002` — Harness de teste MariaDB descartável.
- `T-005` — Backend de suporte GLPI + Tactical completo (publicado em `origin/main`).
- `T-006` — Frontend do Painel de Suporte completo e validado em mocks (publicado em `origin/main` no commit `db1ee3f`, fechamento documental `31a2d1b`).
- `T-007 (Planejamento)` — Especificação formal detalhada em `docs/tasks/T-007-HOMOLOGACAO-HARDENING-DEPLOY.md`:
  - Caminhos reais confirmados no repositório (`cmd/server/license_test.go`, `cmd/server/supportapi.go`, etc.).
  - Metodologia de diagnóstico para o teste de licença (reprodução em UTC e America/Sao_Paulo, limites de data, sem premissas antecipadas).
  - Tabela estrita de governança e autorizações prévias por etapa.
  - Comando conceitual de E2E dedicado (`npm --prefix client run test:e2e`) com portas dinâmicas e encerramento limpo.
  - Segregação rigorosa de 3 ambientes: Homologação Pessoal de Ricardo, Homologação Institucional de Serrana e Produção.
  - Protocolo de encerramento por operação oficial confirmada ou registro manual (sem SQL direto).

## Decisões Pendentes

- **Estratégia de Navegação do Link GLPI (Etapa 7.2):** Deliberação entre Opção A (backend valida contra `WACALLS_GLPI_BASE_URL`), Opção B (backend retorna ID e frontend resolve base segura) ou Opção C (remover ação e manter apenas ID textual; fallback seguro até confirmação de interface web vs API JSON).
- **Arquitetura de Rate Limiting (Etapa 7.5):** Proposta D-019 mantida em aberto como estudo comparativo (Reverse Proxy vs Middleware Persistente vs Idempotency-Key/CAS atual), sem implementação antecipada.

## Bloqueios e Pendências

- **Autorização Prévia Mandatória:** Nenhuma alteração em código de produção, testes ou infraestrutura pode ser executada sem autorização explícita prévia.
- **Baseline Go (Teste de Licença):** `TestLicenseStatusFaixasDeVencimento` em `cmd/server/license_test.go:140` a ser diagnosticado e corrigido deterministicamente na Etapa 7.1.
- **Homologação Real:** Dependente de autorização imediata e credenciais de ambiente para a Etapa 7.4.

## Próximo Passo

- Aguardar autorização formal do usuário para iniciar a **Etapa 7.1** da T-007 (diagnóstico e correção do teste de licença).

## Estado Git

- T-005: Concluída e publicada em `origin/main`.
- T-006: Concluída e publicada em `origin/main`.
- T-007: Planejamento concluído; execução não iniciada.
- Push: Não realizado.
