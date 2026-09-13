# Estado atual

Atualizado em: 2026-09-13
Fase atual: Fase 1 — MVP GLPI + Tactical
Tarefa atual: `T-006` — PLANEJAMENTO TÉCNICO E VISUAL CONCLUÍDO (especificação em `docs/tasks/T-006-PAINEL-SUPORTE-FRONTEND.md`; implementação não iniciada).

## Objetivo imediato

Aprovação das opções de layout e decisões de UX da T-006 antes de autorizar o início da implementação do frontend.

```text
T-005 backend publicado (OK) → T-006 especificação UI/UX (OK) → aprovação de layout (pendente) → implementação T-006
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
  - 8 rotas HTTP mínimas sob `WACALLS_SUPPORT_ENABLED` com validação de payload, detecção de body excessivo e proteção anti-IDOR.
  - Startup recovery síncrono e feature flags em `GET /api/settings/options`.
- `T-006 (especificação técnica e visual do frontend)`:
  - Documento `docs/tasks/T-006-PAINEL-SUPORTE-FRONTEND.md` criado com objetivos, propostas de layout (drawer lateral vs aba), 15 wireframes textuais, arquitetura de componentes, DTOs TypeScript, máquina de estados visual, UX do formulário com Idempotency-Key estável, tratamento de degradação e critérios de aceite.
  - Nenhuma alteração de código realizada (frontend não implementado).

## Bloqueios

- Decisões de produto e UX da T-006 pendentes de aprovação: escolha do layout (drawer lateral vs aba), intervalo de polling, campos iniciais do formulário e comportamento mobile.
- Implementação de código e push: bloqueados até autorização explícita.
- Vínculo nativo Ticket↔Computer: indisponível na API GLPI v2.3; mantido contexto textual.
- `conversation_id`: fora do MVP; bloqueia somente portal futuro.

## Estado Git do checkpoint

- HEAD = `origin/main` = `83fe1b12dfd26befa928c753212d0c306bb71a53` (base da T-005).
- Commit local documental pendente para a especificação da T-006.
- Nenhum push realizado ou autorizado.

## Próximo passo

Aprovação do layout e decisões pendentes da T-006 pelo usuário para liberação da implementação.

## Ambiente preservado

- Windows 11 x64 (`OhMyPi`).
- Go 1.26.4 portátil em `D:/fabrica/WaCalls/toolchains/`.
- Node.js / Vite / React 19 no diretório `client/`.
- Docker Desktop 4.90.0 (WSL2 backend) restrito ao harness de testes locais.
- Nenhuma credencial real no repositório.

## Não tocar nesta etapa

- `client/src/` (nenhum código antes de aprovação formal);
- backend Go (`cmd/server/`, `internal/`) e banco de dados;
- rotas existentes de `messageapi.go` e modelo `(session_id, chat_jid)`;
- automação remota Tactical, retry automático ou worker GLPI;
- chamadas externas reais ou push até autorização.
