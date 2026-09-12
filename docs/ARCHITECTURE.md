# Arquitetura

Este documento descreve a arquitetura **atual confirmada no código** e a
arquitetura **alvo** das integrações. Separa claramente o que existe do que é
intenção.

## Visão lógica alvo

```text
Agente Windows / WhatsApp
            |
            v
   WACalls ServiceOps
     cockpit e orquestração
       /             \
      v               v
   GLPI 11       Tactical RMM
 tickets/ativos   endpoints/ações
```

## Stack atual (confirmado)

- Backend: Go (`module wacalls`, `go 1.26.4`). Servidor em `cmd/server/`,
  migrações em `cmd/migrate/`. HTTP via `net/http` (`http.ServeMux` com padrões
  de rota `GET /api/...{param}`).
- WhatsApp: `go.mau.fi/whatsmeow`. VoIP: `pion/webrtc`. Cache: `redis`.
  Persistência: `modernc.org/sqlite` (SQLite; também há caminho MariaDB nos
  índices compostos).
- Frontend: React 19 + Vite 7 + TypeScript, TailwindCSS, `@xyflow/react`
  (Flow Builder), `@tanstack/react-query`, `zustand`. Em `client/`. Usa `npm`
  (há `bun.lock`, mas `bun` pode não estar instalado).
- Camadas internas: `internal/wa`, `internal/voip`, `internal/storage`,
  `internal/cache`.

### Multitenancy (SaaS) — já existe

O código já implementa isolamento multi-tenant onde **tenant = empresa/conta
raiz**: `currentUser.TenantID()`, `authStore.ListUsersByTenant`,
`SessionManager.tenantOf`, `Broker` com escopo por tenant, `tenant_indexes.go`.
Uma **secretaria não é um tenant**; a hierarquia de secretarias vem do Tactical.
Não reutilizar `tenant_id` para secretaria. Ver `docs/DECISIONS.md` (D-009).

## Domínio de conversas — acoplamento atual

Uma conversa é identificada hoje por `(session_id, chat_jid)`. **Não existe um
`conversation_id` interno independente** (verificado: nenhuma ocorrência no
backend).

As mensagens exigem `session_id` e `chat_jid`. As rotas atuais
(`cmd/server/messageapi.go`) seguem:

```text
GET  /api/sessions/{sid}/chats
GET  /api/sessions/{sid}/chats/{jid}/messages
POST /api/sessions/{sid}/chats/{jid}/send
```

O frontend, o SSE (`Broker`), a listagem de chats e o envio do técnico
pressupõem uma sessão WhatsApp pareada.

**Proibido** mascarar esse acoplamento com sessão portal fictícia, telefone
fictício, UUID gravado como `chat_jid` ou envio de um WhatsApp secundário para o
principal apenas para gerar conversas.

## Flow Builder — separação parcial (confirmado)

Suficientemente separado do WhatsApp (construtor visual, componentes do grafo,
CRUD, persistência, simulador local, armazenamento em SQLite):

```text
client/src/pages/FlowBuilderPage.tsx
client/src/components/domain/flow/   (FlowNodeCard, NodeInspector, FlowSimulator,
                                      node-catalog.ts, simulator.ts)
client/src/services/flows.ts
cmd/server/flowapi.go
cmd/server/flowstore.go
```

Ainda acoplado ao WhatsApp:

```text
cmd/server/flowexec.go     (executor; depende de FlowBridge, dedup de whatsmeow)
cmd/server/flowbridge.go   (mgr *SessionManager, whatsmeow, types.JID,
                            sess.client.SendMessage, mídia WhatsApp)
cmd/server/messageapi.go   (rotas e handlers de chat WhatsApp)
```

O executor depende diretamente de `FlowBridge`, `SessionManager`, `whatsmeow`,
`types.JID` e de mensagens/recursos específicos do WhatsApp. A refatoração
multicanal do executor é a **Fase 4C** e só deve ser avaliada depois do portal.

## Modelos-alvo (intenção — ainda não implementados)

Não colocar campos GLPI/Tactical diretamente na conversa genérica. Separar
conceitualmente comunicação, solicitação e equipamento:

```text
conversations            (Fase 4A — identidade de conversa independente de canal)
- id
- channel                (ex.: whatsapp | agent_portal)
- connection_id
- external_peer_id
- requester_id           (opcional)
- status
- assigned_user_id
- queue_id
- created_at
- updated_at

support_requests         (domínio de suporte — separado da conversa)
- id
- conversation_id
- requester_name
- source_device_id
- target_device_id
- glpi_ticket_id
- category_id
- status
- created_at
- updated_at

device_bindings          (vínculo GLPI/Tactical do equipamento)
- hostname
- hostname_normalized
- tactical_agent_id
- glpi_computer_id
- tactical_client_id
- tactical_site_id
- sector_code
- last_verified_at
```

Relação:

```text
conversation    -> comunicação
support_request -> solicitação e ticket
device_binding  -> equipamento GLPI/Tactical
```

Não adicionar `tenant_id` automaticamente a esses modelos: secretaria não é
tenant. Só usar multitenancy nova mediante decisão explícita.

Estados sugeridos para o casamento de equipamento (`match_status`):
`pending`, `matched`, `conflict`, `missing_glpi`, `missing_tactical`, `disabled`.

## Integrações no backend (alvo)

Adapte os nomes à estrutura real após o inventário/T-002:

```text
integrations/
  glpi/       cliente, autenticação, tickets, followups, ativos
  tactical/   cliente, agentes, status, scripts, remoto
support/      vínculos, abertura, orquestração e auditoria
```

Cada cliente externo deve ter interface própria, timeout, erros tipados e mocks
para testes. GLPI e Tactical **não** passam pelo Flow Builder no MVP.

## Fluxo de abertura (alvo)

1. Agente abre o portal com token curto/descartável ou contexto assinado.
2. Backend valida a origem e resolve o equipamento.
3. Usuário informa nome e escolhe "este computador" ou "outro".
4. Para o computador atual, o agente preenche o hostname automaticamente.
5. Para outro computador ou abertura por WhatsApp, o usuário informa o hostname
   do card/etiqueta; o sistema normaliza e exige correspondência exata.
6. Havendo correspondência única, o backend resolve `tactical_agent_id`,
   `glpi_computer_id`, Client, Site e setor.
7. Sem correspondência única, o chamado vai para triagem, sem associação
   automática; nunca vincular por aproximação silenciosa.
8. Usuário escolhe categoria e descreve o problema.
9. Backend cria uma solicitação idempotente.
10. WACalls cria ou associa conversa.
11. GLPI recebe o ticket e o ativo vinculado.
12. Painel do WACalls mostra ticket e status do Tactical.

## Painel do técnico (alvo)

Painel lateral retrátil na conversa: solicitante/unidade/setor; ticket GLPI,
status, prioridade, SLA; equipamento vinculado e troca; status Tactical; acesso
remoto e abertura nos sistemas externos; ações perigosas separadas e confirmadas.

## Resiliência (alvo)

- Chave de idempotência na abertura do chamado.
- Falha no GLPI não apaga a solicitação local; marcar `pending` e permitir retry.
- Não fazer retry automático de acesso remoto, reboot ou scripts.
- Cache de status Tactical com TTL curto, indicando quando está desatualizado.

## Auditoria (alvo)

Registrar ator, ação, destino, ticket/equipamento, resultado e horário para:
criar/alterar/resolver ticket; vincular/trocar equipamento; abrir acesso remoto;
executar script; transferir atendimento.
