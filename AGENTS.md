# Instruções para agentes de IA — WACalls ServiceOps

Este arquivo é a porta de entrada obrigatória para qualquer agente de IA que
trabalhe neste repositório. Leia-o inteiro antes de qualquer outra coisa.

O repositório permanece `wacalls`. O produto se chama **WACalls ServiceOps**.

## 1. Ordem obrigatória de leitura

1. `AGENTS.md` (este arquivo).
2. `docs/STATUS.md` — estado atual e próxima tarefa pronta.
3. Apenas o arquivo da tarefa ativa em `docs/tasks/` e os arquivos de código
   diretamente relacionados a ela.

Não percorra o repositório inteiro por padrão. Não leia dependências
vendorizadas, artefatos de build, `node_modules`, logs, uploads ou bancos.
Use busca por nomes, símbolos e rotas antes de abrir arquivos grandes.

## 2. Fontes de verdade

- Estado e próxima ação: `docs/STATUS.md`.
- Objetivo e limites do produto: `docs/PROJECT.md`.
- Componentes, acoplamentos atuais e integrações: `docs/ARCHITECTURE.md`.
- Padrão de nomes dos computadores: `docs/HOSTNAMES.md`.
- Ordem das entregas e fases: `docs/ROADMAP.md`.
- Decisões duráveis: `docs/DECISIONS.md`.
- Segurança mínima: `docs/SECURITY.md`.
- Escopo do MVP GLPI+Tactical: `docs/specs/MVP-INTEGRACAO.md`.
- Portal bidirecional do agente Windows: `docs/specs/PORTAL-AGENTE.md`.

Em caso de conflito, **o código executável e os testes demonstram o
comportamento atual**; os documentos definem a intenção. Informe a divergência
antes de ampliá-la; não trate suposição como fato.

## 3. Protocolo antes de editar

1. Identifique a tarefa ativa em `docs/STATUS.md`.
2. Localize os arquivos relevantes com busca direcionada.
3. Descreva um plano de no máximo 7 itens.
4. Liste riscos, migrações, integrações externas e arquivos que pretende alterar.
5. Se faltar uma decisão que mude arquitetura, dados ou segurança, pergunte;
   não invente. Não crie decisões ausentes.

## 4. Protocolo durante a implementação

- Execute apenas uma tarefa principal por sessão.
- Faça mudanças pequenas e verificáveis.
- Preserve comportamento e alterações do usuário não relacionadas.
- Não misture funcionalidade nova com refatoração ampla.
- Não altere contratos de API ou banco sem migração aditiva e compatibilidade
  planejadas (ver §6).
- Use feature flag para integração incompleta ou de risco.
- Nunca grave credenciais, tokens, telefones reais ou dados pessoais em código,
  logs, fixtures ou documentação.
- Não execute ações reais em WhatsApp, GLPI ou Tactical durante testes.
- Não altere produção sem autorização explícita.

## 5. Validação obrigatória

Ao terminar, execute a validação aplicável e registre o que foi feito:

- Backend Go: `go build ./cmd/server` e `go test ./...`.
- Frontend: `npm --prefix client run build` (o client usa `npm`; existe
  `bun.lock`, mas `bun` pode não estar instalado no ambiente).
- Registre explicitamente cada comando **não executado** e o motivo
  (por exemplo: `go` indisponível no ambiente atual — ver `docs/STATUS.md`).

Nunca marque uma tarefa como concluída sem validação ou evidência.

## 6. Verdades do código (confirmadas em 2026-09-11)

Estas são restrições reais do código atual, verificadas na fonte:

- Módulo Go `wacalls`, `go 1.26.4` (ver `go.mod`).
- **Multitenancy SaaS já existe** no código: `tenant` = empresa/conta raiz
  (`currentUser.TenantID()`, `ListUsersByTenant`, `tenant_indexes.go`). Uma
  secretaria **não** é um tenant. Não reaproveitar `tenant_id` para representar
  secretaria; usar a hierarquia do Tactical (Client/Site/Agent).
- Uma conversa é identificada hoje por `(session_id, chat_jid)`. **Não existe
  `conversation_id` interno** (confirmado: nenhuma ocorrência no backend).
- Rotas atuais de chat (em `cmd/server/messageapi.go`):
  - `GET /api/sessions/{sid}/chats`
  - `GET /api/sessions/{sid}/chats/{jid}/messages`
  - `POST /api/sessions/{sid}/chats/{jid}/send`
- Flow Builder tem separação **parcial**:
  - Já separado (visual, grafo, CRUD, persistência SQLite, simulador):
    `client/src/pages/FlowBuilderPage.tsx`, `client/src/components/domain/flow/`,
    `client/src/services/flows.ts`, `cmd/server/flowapi.go`,
    `cmd/server/flowstore.go`.
  - Ainda acoplado ao WhatsApp: `cmd/server/flowexec.go`,
    `cmd/server/flowbridge.go`, `cmd/server/messageapi.go`. O executor depende de
    `FlowBridge`, `SessionManager`, `whatsmeow`, `types.JID` e mensagens
    específicas do WhatsApp.
- O `Conversation Core` (Fase 4A) **bloqueia** o portal bidirecional (Fase 4B),
  mas **não bloqueia** o MVP GLPI+Tactical sobre as conversas WhatsApp atuais
  (Fases 1–3). Ver `docs/ROADMAP.md` e `docs/DECISIONS.md`.

## 7. Regras do produto

- WACalls ServiceOps é o cockpit de atendimento e comunicação.
- GLPI é a fonte oficial de tickets, SLA, categorias, locais, solicitantes e ativos.
- Tactical RMM é a fonte operacional de status, agente, scripts e acesso remoto.
- O agente Windows fornece identidade e contexto do endpoint.
- No Tactical: Client = secretaria, Site = unidade/departamento, Agent = equipamento.
- A estrutura organizacional implantada vem do Tactical; não manter cadastro
  paralelo manual sem necessidade.
- Não recriar GLPI ou Tactical dentro do WACalls.
- O hostname identifica secretaria, unidade, setor e equipamento conforme
  `docs/HOSTNAMES.md`; serve para busca e vínculo visível, mas não substitui os
  IDs permanentes (`glpi_computer_id`, `tactical_agent_id`).
- A pessoa pode abrir chamado para outro equipamento; o vínculo chamado↔equipamento
  deve ser explícito e auditável.

## 8. Proibições sem autorização explícita

- Alterar infraestrutura de produção ou rodar migração destrutiva.
- Expor GLPI, Tactical ou banco diretamente à internet.
- Enviar mensagens reais de WhatsApp em testes.
- Executar script ou acesso remoto automaticamente no computador do usuário.
- Trocar stack, banco ou arquitetura principal.
- Criar sessão portal fictícia, telefone fictício, UUID armazenado como
  `chat_jid`, ou enviar um WhatsApp secundário para o principal apenas para gerar
  conversas — isso esconderia o acoplamento e faria máquinas diferentes
  compartilharem a mesma identidade.
- Implementar renomeação automática ou em massa de hostnames.

## 9. Economia de tokens

- Leia `AGENTS.md` e `docs/STATUS.md` antes de qualquer varredura de código.
- Prefira busca por símbolo/rota a abrir diretórios inteiros.
- Abra apenas os trechos relevantes.
- Não repita a arquitetura completa em cada resposta; referencie os documentos.
- Não cole logs completos.
- Não reanalise decisões já registradas em `docs/DECISIONS.md`.
- Não crie documentos duplicados nem changelog manual extenso — o histórico
  detalhado vive no Git.
- Uma sessão executa uma tarefa principal.

## 10. Definição de pronto

Uma tarefa só está pronta quando:

- os critérios de aceitação da tarefa foram atendidos;
- a validação aplicável passou (ou o motivo de não executá-la está registrado);
- falhas externas têm tratamento compreensível;
- não há segredo no diff;
- a documentação mínima foi atualizada;
- existe uma próxima ação clara em `docs/STATUS.md`.

## 11. Obrigação ao terminar

1. Execute testes, lint e build aplicáveis (ou registre por que não).
2. Resuma arquivos alterados, comportamento entregue, testes e pendências.
3. Atualize `docs/STATUS.md` de forma curta (mantê-lo abaixo de ~150 linhas).
4. Atualize `docs/ROADMAP.md` somente se uma fase ou marco mudou.
5. Registre em `docs/DECISIONS.md` somente se houve nova decisão durável.
6. Registre o resultado no arquivo da tarefa em `docs/tasks/`.
7. Deixe a próxima tarefa pronta e apontada em `docs/STATUS.md`.
8. Não reescreva documentos que não mudaram.
