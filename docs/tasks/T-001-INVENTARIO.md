# T-001 — Inventário técnico do repositório

Status: parcialmente concluída (baseline confirmado durante a consolidação da
documentação em 2026-09-11). O inventário funcional completo (frontend, agente
Windows, APIs externas) permanece a confirmar em T-002.

## Resultado esperado

Produzir um mapa confiável do WACalls e do agente Windows, com comandos reais de
validação e pontos exatos onde o MVP será implementado, sem alterar comportamento
funcional.

## Contexto mínimo

- Documentos: `AGENTS.md`, `docs/STATUS.md`, `docs/PROJECT.md`,
  `docs/ARCHITECTURE.md`, `docs/specs/MVP-INTEGRACAO.md`.
- Módulo provável: `cmd/server/*`, `internal/*`, `client/src/*`.

## Dentro do escopo

- Estrutura, linguagens, versões, build/test/lint, containers.
- Backend: entrada, roteamento, auth, domínio de conversas/filas/conexões,
  banco/migrations, integrações existentes.
- Frontend: entrada, roteamento, tela de chat, estado, permissões.
- Agente Windows: origem de hostname/IP/patrimônio, mecanismo do botão de chamado.
- Estrutura Tactical/hostnames: endpoints/campos de Client/Site/Agent.

## Fora do escopo

- Instalar dependências globalmente; alterar código de produção; criar
  tabelas/endpoints/componentes; corrigir bugs; reformatar; atualizar
  dependências; executar chamadas mutáveis em GLPI/Tactical/WhatsApp.

## Critérios de aceitação

1. Uma nova sessão inicia a Fase 1 lendo `AGENTS.md` e `docs/STATUS.md`.
2. Nenhum arquivo funcional foi alterado.
3. Não há suposição técnica apresentada como fato.

## Plano de validação

- Comandos de leitura e validação local segura.
- Registrar cada comando de build/test não executado e o motivo.
- Não expor `.env`, tokens, cookies ou dados reais.

## Resultado obtido (2026-09-11)

Confirmado por leitura direta do código:

- Backend Go, módulo `wacalls`, `go 1.26.4`. Servidor em `cmd/server/`,
  migrações em `cmd/migrate/main.go`.
- HTTP via `http.ServeMux`. Rotas de chat em `cmd/server/messageapi.go`:
  `GET /api/sessions/{sid}/chats`,
  `GET /api/sessions/{sid}/chats/{jid}/messages`,
  `POST /api/sessions/{sid}/chats/{jid}/send` (e variantes assign/close/media/...).
- Conversa identificada por `(session_id, chat_jid)`; **não existe**
  `conversation_id` no backend.
- Multitenancy SaaS já existe (tenant = empresa): `TenantID()`,
  `ListUsersByTenant`, `tenant_indexes.go`. Secretaria ≠ tenant.
- Flow Builder com separação parcial (ver `docs/ARCHITECTURE.md`): separado em
  `flowapi.go`/`flowstore.go` e client; acoplado ao WhatsApp em `flowexec.go`,
  `flowbridge.go` (`SessionManager`, `whatsmeow`, `types.JID`), `messageapi.go`.
- Frontend React 19 + Vite 7 + TS em `client/`, usa `npm`.
- Toolchain do ambiente atual: `node v24.21.0` presente; **`go` e `bun`
  ausentes** — `go build`/`go test` não executados nesta sessão.

Pendente para T-002: mapa detalhado de frontend (tela de chat/estado/permissões),
agente Windows, feature flags, mecanismo de retry/worker e endpoints reais de
GLPI/Tactical.
