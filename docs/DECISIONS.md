# Registro de decisões

Registre somente decisões com efeito durável de arquitetura ou produto. Não use
este arquivo como diário de desenvolvimento.

## D-001 — Sistemas especialistas continuam como fonte de verdade

Data: 2026-09-11 · Status: aceita

Decisão: WACalls será o cockpit e orquestrador; GLPI continua como fonte oficial
de tickets/ITSM e Tactical como fonte operacional de endpoints.
Motivo: reduz duplicação, aproveita sistemas já implantados e limita o escopo.
Consequência: o WACalls precisa tolerar indisponibilidade e divergência
temporária entre integrações.

## D-002 — Hostname não será a única chave permanente

Data: 2026-09-11 · Status: aceita

Decisão: hostname serve para descoberta, mas o vínculo persiste
`glpi_computer_id` e `tactical_agent_id`, além do patrimônio quando disponível.
Motivo: computadores podem ser renomeados e cadastros podem divergir.
Consequência: será necessário processo de matching, revisão de conflitos e
verificação periódica.

## D-003 — Equipamento afetado pertence ao atendimento/chamado

Data: 2026-09-11 · Status: aceita

Decisão: o computador de origem pode sugerir o alvo, mas o equipamento afetado é
vinculado ao atendimento/ticket e pode ser trocado.
Motivo: uma pessoa pode solicitar suporte para outro computador.
Consequência: o modelo separa `source_device` de `target_device`.

## D-004 — Dois números com responsabilidades distintas

Data: 2026-09-11 · Status: proposta (validar operação e política do WhatsApp)

Decisão proposta: um número para atendimento humano e outro para
alertas/automações.
Motivo: impede que alertas e disparos poluam a fila de suporte.
Consequência: conexões, templates, permissões e monitoramento serão separados.

## D-005 — Nome e abrangência municipal

Data: 2026-09-11 · Status: aceita

Decisão: o produto se chama `WACalls ServiceOps`, descrição "Central unificada de
atendimento, chamados e gestão de dispositivos", e atenderá toda a Prefeitura de
Serrana. O repositório permanece `wacalls`.
Motivo: o escopo não se limita à Saúde.
Consequência: a Saúde é a implantação inicial, não o limite do produto.

## D-006 — Tactical fornece a estrutura implantada

Data: 2026-09-11 · Status: aceita

Decisão: no Tactical, Client = secretaria, Site = unidade/departamento,
Agent = equipamento. O ServiceOps sincroniza essa hierarquia.
Motivo: a organização já existe no Tactical e não deve ser recadastrada.
Consequência: IDs de Client, Site e Agent são preservados nos vínculos locais.

## D-007 — Hostname identifica o setor operacional

Data: 2026-09-11 · Status: aceita

Decisão: o hostname é o identificador visível do equipamento e codifica
secretaria, unidade, setor e número; o chamado é associado ao hostname e aos IDs
permanentes Tactical/GLPI.
Motivo: o padrão atual permite localizar rapidamente onde e para que o
computador é usado.
Consequência: nomes legados serão corrigidos manualmente, um por vez; o parser
tolera aliases durante a transição.

## D-008 — Card e etiqueta fornecem o código do equipamento

Data: 2026-09-11 · Status: aceita

Decisão: o hostname é exibido no card do agente e em etiqueta no gabinete. O
usuário pode informá-lo ao abrir chamado; correspondência exata resolve o ativo
GLPI e o Agent Tactical.
Motivo: identificar o computador e seu setor sem depender de telefone, IP,
patrimônio ou descrição informal.
Consequência: hostname inexistente ou ambíguo vai para triagem, nunca associado
por aproximação automática.

## D-009 — Multitenancy existe como empresa; secretaria não é tenant

Data: 2026-09-11 · Status: aceita

Decisão: o código já possui multitenancy SaaS onde `tenant` = empresa/conta raiz
(`TenantID()`, `ListUsersByTenant`, `tenant_indexes.go`). Uma **secretaria não é
um tenant**. A hierarquia de secretarias vem do Tactical (Client/Site/Agent).
Motivo: verificado no código; reaproveitar `tenant_id` para secretaria
conflitaria o isolamento SaaS com a organização municipal.
Alternativas consideradas: adicionar `tenant_id` por secretaria — rejeitada.
Consequência: os modelos de suporte (conversation, support_request,
device_binding) não recebem `tenant_id` por secretaria; multitenancy nova só com
decisão explícita futura (ex.: várias prefeituras na mesma instalação).

## D-010 — MVP GLPI+Tactical não cria conversation_id nem passa pelo Flow Builder

Data: 2026-09-11 · Status: aceita

Decisão: as Fases 1–3 (MVP) rodam sobre as conversas WhatsApp atuais
`(session_id, chat_jid)`. Não criar `conversation_id` nem rotear GLPI/Tactical
pelo Flow Builder neste bloco. O Flow Builder atual permanece funcionando.
Motivo: entregar valor sem a migração de identidade de conversa; menor risco.
Consequência: o painel GLPI/Tactical é adicionado à tela de conversa existente,
como domínio de suporte separado.

## D-011 — Conversation Core precede o portal; ordem 4A → 4B → 4C

Data: 2026-09-11 · Status: aceita

Decisão: criar `conversation_id` independente de canal (Fase 4A) antes do portal
bidirecional (Fase 4B); a refatoração multicanal do Flow Builder (Fase 4C) só
depois do portal funcionar. A migração de 4A é aditiva e não remove o modelo
antigo até validação.
Motivo: o portal exige identidade de conversa fora do WhatsApp; o executor de
fluxo está acoplado ao WhatsApp.
Consequência: o Conversation Core **bloqueia** o portal (Fase 4B), mas **não
bloqueia** o MVP (Fases 1–3). Ver `docs/ROADMAP.md`.

## D-012 — Proibição de identidades falsas de conversa

Data: 2026-09-11 · Status: aceita

Decisão: proibido criar sessão portal fictícia, telefone fictício, UUID gravado
como `chat_jid`, ou enviar um WhatsApp secundário para o principal só para gerar
conversas.
Motivo: esconderia o acoplamento e faria máquinas diferentes compartilharem a
mesma identidade.
Consequência: o portal exige o Conversation Core real (Fase 4A) antes de existir.

## D-013 — Domínio de suporte separado, ligado à conversa por (session_id, chat_jid)

Data: 2026-09-11 · Status: aceita

Decisão: o MVP cria tabelas próprias `support_requests` e `device_bindings`, sem
colocar campos GLPI/Tactical na conversa. `support_requests` referencia a conversa
atual por `(session_id, chat_jid)` — **não** por `conversation_id`. As duas
tabelas carregam `owner_id`/`tenant_id` = empresa (SaaS), como `scheduled_messages`
e `quick_replies`; isso é isolamento por empresa, não por secretaria (D-009).
Motivo: reaproveita o isolamento SaaS existente e mantém o domínio de suporte
desacoplado do chat.
Alternativas consideradas: criar `conversation_id` agora — rejeitada (é a Fase 4A,
D-011); embutir campos no chat — rejeitada (acopla domínios).
Consequência: quando o Conversation Core (4A) existir, `support_requests` ganha
`conversation_id` de forma aditiva, mantendo `(session_id, chat_jid)` na transição.

## D-014 — Clientes GLPI/Tactical em internal/ atrás de interfaces, com retry mínimo

Data: 2026-09-11 · Status: aceita

Decisão: as integrações ficam em `internal/glpi` e `internal/tactical` (padrão de
`internal/wa`, `internal/voip`), com `http.Client` de timeout, erros tipados e base
URL/token via env `WACALLS_*`. O `cmd/server` consome via interfaces
(`glpiClient`/`tacticalClient`) para permitir mocks nos testes. Falha de GLPI não
perde a solicitação local: `sync_state='sync_error'` + retry idempotente por um
ticker com backoff e teto de tentativas. Tactical é somente leitura com cache TTL curto.
Motivo: isolar dependências externas, testar sem APIs reais e tolerar
indisponibilidade (D-001).
Alternativas consideradas: chamar as APIs direto no handler — rejeitada (sem mock,
sem resiliência); fila/worker dedicado — adiada (excesso para o MVP).
Consequência: nenhuma ação remota/reboot/script no MVP; GLPI só create/get,
Tactical só status/busca.

## Modelo para novas decisões

```text
## D-NNN — Título
Data: AAAA-MM-DD
Status: proposta | aceita | substituída
Decisão:
Motivo:
Alternativas consideradas:
Consequências:
```
