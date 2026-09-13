# Estado atual

Atualizado em: 2026-09-13
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa atual: `T-006` — IMPLEMENTAÇÃO DO PAINEL DE SUPORTE CONCLUÍDA LOCALMENTE (build Vite OK, testes 100% aprovados; sem push).

## Objetivo imediato

Apresentar a revisão e os testes da implementação frontend da T-006 para validação do usuário.

```text
T-005 backend publicado (OK) → T-006 especificação UI/UX (OK) → implementação T-006 (OK) → revisão / autorização de push (pendente)
```

## Concluído

- `T-001` — inventário/baseline do código.
- `T-002` — plano técnico do slice; decisões D-013/D-014.
- `T-B001` — pacote `internal/voip/media` recuperado e versionado.
- `T-003` — normalização de hostname e store `device_bindings`, com SQLite.
- `T-004` — clientes `internal/glpi` e `internal/tactical`, testes offline.
- `chore` — `.gitattributes` multiplataforma com política explícita de EOL.
- `T-B002` — harness MariaDB descartável validado 100% em SQLite e MariaDB 11.4.
- `T-005 (backend de suporte completo)` — concluída e publicada em `origin/main`:
  - DDL e integridade referencial com FK composta (SQLite e MariaDB).
  - GLPI `GetTicket` v2.3 com sanitização e renovação OAuth.
  - `SupportService` com ciclo fechado, CAS, anti-duplicação e consulta estritamente tenant-scoped (`GetForTenant`).
  - 8 rotas HTTP sob `WACALLS_SUPPORT_ENABLED` com validação de payload e proteção anti-IDOR.
- `T-006 (especificação e implementação frontend)`:
  - Especificação em `docs/tasks/T-006-PAINEL-SUPORTE-FRONTEND.md` com baseline documental commit `a8e19f4fe0d49f5ca84f9db02eb0b7b720afbe6a`.
  - Painel lateral (Opção 1: Sheet à direita) integrado em `ChatView.tsx` preservando 100% a timeline de mensagens.
  - Botão de suporte condicionado a `features.support = true`.
  - DTOs em `types/support.ts` e API client em `services/support.ts` consumindo os 8 endpoints.
  - Idempotency-Key estável (`ui-<uuid>`) preservada entre renderizações e com bloqueio anti-duplo clique.
  - Cobertura completa dos 5 estados (`processing`, `synced`, `retryable_error`, `unknown`, `failed`).
  - Retry restrito a `retryable_error` com suporte a HTTP 429 `Retry-After`.
  - Reconciliação restrita a administradores (`isAdmin(user)`) em estado `unknown`.
  - Troca de equipamento via `DevicePickerModal` com busca debounced e atualização imediata.
  - Telemetria Tactical exibida apenas com `features.tactical = true` e fallback seguro com aviso amigável quando indisponível.
  - Suite de testes frontend (`client/tests/support.test.mjs`) cobrindo todos os 7 fluxos críticos com 100% de sucesso.
  - Build de produção (`vite build`) e testes Go de regressão (`go test ./...`) 100% verdes.

## Bloqueios

- Vínculo nativo Ticket↔Computer: indisponível na API GLPI v2.3; mantido contexto textual.
- `conversation_id`: fora do MVP; bloqueia somente portal futuro.
- Push remoto: bloqueado até autorização formal do usuário.

## Estado Git do checkpoint

- HEAD local: commit de documentação (`a8e19f4`) seguido pelo commit de implementação da T-006.
- Nenhum push realizado ou autorizado.

## Próximo passo

Revisão da implementação pelo usuário e autorização para publicação/push quando oportuno.

## Ambiente preservado

- Windows 11 x64 (`OhMyPi`).
- Go 1.26.4 portátil em `D:/fabrica/WaCalls/toolchains/`.
- Node.js 24.21.0 / Vite 7 / React 19 no diretório `client/`.
- Docker Desktop 4.90.0 (WSL2 backend) restrito ao harness de testes locais.
- Nenhuma credencial real no repositório.

## Não tocar nesta etapa

- backend Go (`cmd/server/`, `internal/`) e banco de dados;
- rotas existentes de `messageapi.go` e modelo `(session_id, chat_jid)`;
- automação remota Tactical, retry automático ou worker GLPI;
- chamadas externas reais ou push até autorização.
