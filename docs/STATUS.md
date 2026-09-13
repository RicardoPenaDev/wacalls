# Estado atual

Atualizado em: 2026-09-13
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa atual: `T-006` — Painel de suporte frontend implementado e validado em mocks locais.

## Objetivo imediato

Registrar o fechamento documental da T-006 e planejar a T-007 (fechamento, hardening, runbook e deploy da Fase 1).

```text
T-005 backend (publicado) → T-006 frontend (implementado e validado com mocks) → T-007 planejamento (não iniciada)
```

## Concluído

- `T-001` — Inventário e baseline do código.
- `T-002` — Plano técnico do slice (decisões D-013/D-014).
- `T-B001` — Recuperação do pacote `internal/voip/media`.
- `T-003` — Normalização de hostnames e store `device_bindings` (SQLite/MariaDB).
- `T-004` — Clientes HTTP `internal/glpi` e `internal/tactical` com testes offline.
- `T-B002` — Harness de teste MariaDB descartável.
- `T-005` — Backend de suporte GLPI + Tactical completo (publicado em `origin/main`):
  - DDL com chaves compostas e integridade referencial.
  - GLPI 11.0.8 / High-Level REST API v2.3 (`GetTicket`) e `SupportService` atômico com idempotência.
  - Rotas sob `WACALLS_SUPPORT_ENABLED` e isolamento multi-tenant.
- `T-006` — Frontend do Painel de Suporte (commit `db1ee3f`):
  - Componentes: `SupportPanel`, `SupportTicketForm`, `SupportRequestStatus`, `DeviceSummary`, `TacticalStatus`, `DevicePickerModal`, `RetryAction`, `ReconcileDialog`.
  - Integração aditiva ao `ChatView.tsx` sob `features.support = true`.
  - Preservação integral do rascunho de mensagem e timeline no WhatsApp.
  - Fallback sem telemetria quando `features.tactical = false`.
  - Cobertura de estados: `processing`, `synced`, `retryable_error`, `unknown`, `failed`.
  - Reconciliação em estado `unknown` restrita a administradores (`isAdmin(user)`).

## Validações Realizadas

1. **Frontend Build:** `npx vite build` executado com sucesso (zero erros).
2. **Frontend Unitários:** `npm run test` executando `client/tests/support.test.mjs` (7/7 aprovados).
   - Inclui validação estrita de rede e idempotência contra disparos concorrentes de requisição.
3. **Testes Direcionados de Suporte / Backend:**
   - Compilação `go build ./...` aprovada.
   - `go test -run Support ./cmd/server` e testes dos pacotes `internal/glpi` e `internal/tactical` 100% aprovados.
4. **Validação E2E Local (com Mocks):**
   - 7/7 cenários validados via navegador real contra backend local e mocks HTTP de GLPI 11.0.8 / High-Level REST API v2.3 e Tactical RMM em loopback.
   - Execução via script temporário de apoio (não versionado).
   - Validou: abertura do painel com dispositivo vinculado, transição atômica `processing` → `synced`, retenção de rascunho de chat, comportamento visual/operacional anti-duplo clique (botão desabilitado e estado de envio), resposta a 429 (`Retry-After`), fallback de telemetria desabilitada e controle de acesso a reconciliação (operador sem ação vs admin com modal).
5. **Navegação "Abrir no GLPI":**
   - Navegação estrita por link (`target="_blank"` com `href` para a URL do chamado retornada pelo backend).
   - Não manipula nem armazena credenciais do GLPI no navegador; depende de sessão/login prévio do usuário no GLPI.
   - Item mapeado para revisão de hardening na T-007 (ex.: validação de URL e atributos de segurança).

## Pendências e Observações

- **Suíte Go Global (Baseline Pré-existente):** `go test ./...` apresenta divergência pré-existente de timezone de 1 dia em `TestLicenseStatusFaixasDeVencimento` (`license_test.go:181`). Sem relação com o suporte, mas registrada como pendência de baseline a ser investigada antes da T-007.
- **Homologação em Ambiente Real:** Validações de T-006 foram conduzidas em ambiente simulado com mocks locais. Homologação com servidores reais de GLPI 11.0.8 e Tactical RMM permanece pendente para ambiente controlado de homologação/staging.
- **Vínculo nativo Ticket ↔ Computer:** Indisponível na High-Level REST API v2.3 do GLPI (mantido vínculo lógico no banco local e contextual em texto do ticket).

## Próximo Passo

- Planejamento da `T-007` — Hardening de segurança, auditoria, telemetria, runbook e documentação final de encerramento da Fase 1 (execução da T-007 ainda não iniciada).

## Estado Git

- T-005: Concluída e publicada em `origin/main`.
- T-006: Implementada e publicada em `origin/main` (commit `db1ee3f`), com validação E2E local em mocks concluída.
- T-007: Não iniciada.
- Push: Não realizado.
