# Estado atual

Atualizado em: 2026-09-13
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa atual: `T-007` — Etapa 7.1 concluída com sucesso (suíte Go global 100% verde).

## Objetivo imediato

Aguardar autorização formal do usuário para iniciar a execução da Etapa 7.2 da T-007 (hardening do link GLPI).

```text
T-005 backend (publicado) → T-006 frontend (publicado) → T-007 (Etapa 7.1 concluída → Etapa 7.2 pendente de autorização)
```

## Concluído

- `T-001` — Inventário e baseline do código.
- `T-002` — Plano técnico do slice (decisões D-013/D-014).
- `T-B001` — Recuperação do pacote `internal/voip/media`.
- `T-003` — Normalização de hostnames e store `device_bindings` (SQLite/MariaDB).
- `T-004` — Clientes HTTP `internal/glpi` e `internal/tactical` com testes offline.
- `T-B002` — Harness de teste MariaDB descartável.
- `T-005` — Backend de suporte GLPI + Tactical completo (publicado em `origin/main`).
- `T-006` — Frontend do Painel de Suporte completo e validado em mocks (publicado em `origin/main`).
- `T-007 (Planejamento)` — Especificação formal em `docs/tasks/T-007-HOMOLOGACAO-HARDENING-DEPLOY.md`.
- `T-007 (Etapa 7.1 — Teste de Licença Determinístico)`:
  - Causa confirmada: drift de relógio de parede entre definição da tabela e execução dos subtestes somado ao truncamento da divisão inteira de segundos (`/ 86400`).
  - Correção aplicada: injeção de clock controlável `licenseNow` (`var licenseNow = time.Now`), captura de instante único (`now := licenseNow()`) por operação composta (`licenseStatus`, `renewLicense`, `checkLicense`) via helpers `verifyLicenseAt` e `diasParaVencerAt`, eliminando janelas de drift ou inconsistência.
  - Mock e isolamento: helpers `mockLicenseClock` e `mockLicenseFlagPath` com restauração automática via `t.Cleanup`; garantia de proibição de `t.Parallel` em testes que mutam estado global de pacote.
  - Remoção de fallback de fuso: `TestLicenseStatusTimezonesUTCeSP` exige timezone nativo `America/Sao_Paulo` com `t.Fatalf` em caso de erro, sem fallback para `FixedZone`.
  - Testes adicionados: limites exatos e instantes adjacentes (`TestLicenseStatusLimitesEInstantes`), virada de dia/mês/ano/bissexto (`TestLicenseStatusViradaDeDiaEData`), consistência UTC vs America/Sao_Paulo (`TestLicenseStatusTimezonesUTCeSP`) e comprovação de consulta única ao relógio (`TestLicenseStatusConsultaRelogioUmaUnicaVez`).
  - Validação: `go test ./...` 100% verde em todos os pacotes; `go test ./cmd/server -run '^TestLicense' -count=100` sem flakes.


## Decisões Pendentes

- **Estratégia de Navegação do Link GLPI (Etapa 7.2):** Deliberação entre Opção A (backend valida contra `WACALLS_GLPI_BASE_URL`), Opção B (backend retorna ID e frontend resolve base segura) ou Opção C (remover ação e manter apenas ID textual; fallback seguro até confirmação de interface web vs API JSON).
- **Arquitetura de Rate Limiting (Etapa 7.5):** Proposta D-019 mantida em aberto como estudo comparativo (Reverse Proxy vs Middleware Persistente vs Idempotency-Key/CAS atual), sem implementação antecipada.

## Bloqueios e Pendências

- **Autorização Prévia Mandatória:** Nenhuma alteração em código de produção, testes ou infraestrutura pode ser executada sem autorização explícita prévia para cada etapa.
- **Etapa 7.2:** Hardening do link GLPI aguardando autorização para iniciar.
- **Homologação Real (Etapa 7.4):** Dependente de autorização imediata e credenciais de ambiente para a Etapa 7.4.

## Próximo Passo

- Aguardar autorização formal do usuário para iniciar a **Etapa 7.2** da T-007 (hardening do link GLPI).

## Estado Git

- T-005: Concluída e publicada em `origin/main`.
- T-006: Concluída e publicada em `origin/main`.
- T-007: Etapa 7.1 concluída localmente; Etapa 7.2 não iniciada.
- Push: Não realizado.
