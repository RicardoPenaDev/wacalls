# Roadmap

Estados: `[ ]` não iniciada · `[~]` em andamento · `[x]` concluída · `[!]` bloqueada.

Atualize apenas quando uma fase ou marco mudar. Não registre pequenos detalhes de
implementação aqui — eles vivem no Git e em `docs/tasks/`.

Cada fase deve entregar algo demonstrável e reversível.

## `[~]` Fase 0 — baseline, inventário e documentação

- Mapear repositório, ambientes e comandos reais.
- Consolidar a documentação de continuidade (feito).
- Confirmar contratos das APIs GLPI/Tactical.

Pronto quando: uma nova IA inicia a Fase 1 lendo só `AGENTS.md`,
`docs/STATUS.md` e a tarefa ativa. Tarefas: `T-001` (inventário), `T-002` (plano do slice).

---

## MVP GLPI + Tactical (Fases 1–3)

Manter as conversas WhatsApp funcionando como estão. GLPI e Tactical **não**
passam pelo Flow Builder neste MVP. O Flow Builder atual permanece funcionando.
Não criar `conversation_id` neste bloco.

### `[ ]` Fase 1 — identidade do equipamento (GLPI ↔ Tactical)

- Documentar e validar o padrão de hostname (`docs/HOSTNAMES.md`).
- Criar modelo de vínculo (`device_bindings`) guardando `glpi_computer_id`,
  `tactical_agent_id`, Client/Site e setor.
- Importar/consultar dados de ambos os sistemas.
- Matching inicial por hostname normalizado, com correspondência exata.
- Tela administrativa de conflitos e confirmação manual.
- Corrigir nomes legados manualmente, um computador por vez, após piloto.

Pronto quando: um equipamento confirmado mostra IDs estáveis de ambos os
sistemas, sem depender apenas do hostname.

### `[ ]` Fase 2 — primeiro chamado GLPI pela conversa

- Criar ticket manualmente a partir da conversa WhatsApp atual.
- Selecionar categoria, localização e equipamento.
- Exibir número e status no painel lateral.
- Abrir o ticket no GLPI por link seguro.
- Criação idempotente (não duplicar ticket ao repetir a requisição).

Pronto quando: um técnico cria e consulta um ticket real de homologação sem sair
do fluxo principal.

### `[ ]` Fase 3 — contexto Tactical (somente leitura)

- Exibir online/offline, hostname, IP e informações essenciais.
- Pesquisar equipamentos pela unidade.
- Vincular/trocar o equipamento do chamado com auditoria.
- Botão de acesso remoto conforme a capacidade segura da API.

Pronto quando: o técnico identifica e abre o endpoint correto com auditoria.

Escopo detalhado do MVP: `docs/specs/MVP-INTEGRACAO.md`.

---

## Conversation Core e Portal (Fase 4)

### `[ ]` Fase 4A — Conversation Core independente de canal

Antes do portal bidirecional, criar uma identidade interna de conversa
(`conversation_id`). Migração **aditiva**:

1. Criar entidade genérica `conversations`.
2. Criar uma conversa para cada par atual `(session_id, chat_jid)`.
3. Adicionar `conversation_id` às mensagens.
4. Backfill das mensagens existentes.
5. Manter `session_id` e `chat_jid` durante a transição.
6. Criar endpoints novos baseados em `conversation_id`.
7. Manter os endpoints atuais como compatibilidade.
8. Migrar frontend e SSE gradualmente.
9. Criar dispatchers/adaptadores por canal.
10. Não remover o modelo antigo até a migração estar validada.

Pronto quando: existe `conversation_id` com endpoints novos e os antigos
seguem funcionando; nada de produção quebrou.

### `[!] Fase 4B — Portal bidirecional do agente Windows`

Bloqueada por: Fase 4A (`conversation_id`). O card do Windows ganha o botão
"Abrir chamado", que abre uma página web tipo chat. Cada abertura gera uma
conversa independente (`channel = agent_portal`, `external_id = portal_session_id`).
Detalhes: `docs/specs/PORTAL-AGENTE.md`.

Pronto quando: um funcionário abre chamado do PC atual e de outro PC sem
informar IP ou patrimônio manualmente, e o técnico responde pela mesma dashboard.

### `[ ]` Fase 4C — Flow Builder multicanal

Somente depois do portal funcionar, avaliar a refatoração do executor. Objetivo:
mesmo motor de fluxo com adaptador WhatsApp e adaptador Portal Web, separando
entrada, identidade da conversa, execução do grafo, efeitos, envio por canal,
capacidades de cada canal e persistência do estado. Não criar outro editor
visual agora.

Pronto quando: o mesmo grafo roda em WhatsApp e no Portal sem código duplicado
por canal.

---

## Evolução operacional (pós-portal)

### `[ ]` Fase 5 — sincronização operacional

Acompanhamentos no GLPI, sincronização de transferência/grupo, resolução do
ticket ao finalizar conversa mediante confirmação, tratamento de mensagens
agrupadas e anexos. Pronto quando: não ficam tickets esquecidos por divergência.

### `[ ]` Fase 6 — ações Tactical controladas

Catálogo pequeno de scripts aprovados, confirmação explícita, autorização por
perfil, auditoria e kill switch por feature flag. Pronto quando: scripts
aprovados rodam em homologação com controle de acesso e trilha completa.

### `[ ]` Fase 7 — produção e indicadores

Validar o piloto na Saúde, observabilidade, alertas, runbook e métricas do
produto antes de expandir para outras secretarias. Pronto quando: piloto estável
e decisão registrada de expandir, ajustar ou interromper.

## Regra de priorização

Não iniciar uma fase porque "a anterior parece pronta". Verifique o critério de
conclusão e registre evidência em `docs/STATUS.md`.
