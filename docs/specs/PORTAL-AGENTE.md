# Portal bidirecional do agente Windows (Fase 4B)

Status: **bloqueado** pela Fase 4A (Conversation Core / `conversation_id`).
Não implementar antes de 4A. Ver `docs/ROADMAP.md` e `docs/DECISIONS.md`
(D-011, D-012).

## Objetivo

O card instalado no Windows terá o botão **"Abrir chamado"** (substituindo
"Abrir chamado no GLPI"). Ele abre uma página web semelhante a um chat, com
mensagens, campos e botões, permitindo abrir e acompanhar chamados pelo próprio
computador, sem depender de WhatsApp.

## Pré-requisito arquitetural

O portal exige identidade de conversa independente de canal. Enquanto uma
conversa for identificada por `(session_id, chat_jid)`, não há como representar
uma conversa de portal sem uma identidade falsa — o que é **proibido** (D-012).
Logo, a Fase 4A cria `conversations` com `conversation_id`, `channel` e
`external_peer_id`; o portal usa:

```text
channel      = agent_portal
external_id  = portal_session_id
```

## Fluxo inicial

1. Identificar o computador (contexto assinado do agente).
2. Perguntar o nome do solicitante.
3. Perguntar se o problema é neste computador ou em outro.
4. Se for outro, solicitar o hostname do card/etiqueta.
5. Perguntar a categoria.
6. Coletar a descrição.
7. Mostrar resumo.
8. Confirmar abertura.
9. Criar conversa no WACalls (`channel = agent_portal`).
10. Criar ticket no GLPI (associado ao ativo, quando houver correspondência).
11. Continuar como chat bidirecional entre funcionário e técnico.

## Conversas independentes na mesma dashboard

Cada abertura gera uma conversa independente. O técnico usa a mesma dashboard:

```text
[WhatsApp] Maria — SDE-ARS-RCP-02   -> resposta enviada ao telefone
[Portal]   João  — SDE-BEA-ENF-01   -> resposta enviada à página em tempo real
```

O WhatsApp poderá ser usado opcionalmente para notificações ou atendimento
iniciado pelo próprio WhatsApp, mas **não** será obrigatório para abrir chamado
pelo computador.

## Fluxo fixo pequeno (primeira versão)

A primeira versão do portal pode ter um fluxo fixo pequeno **no código**, desde
que a implementação permita usar o motor de fluxo compartilhado no futuro
(Fase 4C). Não criar outro editor visual agora.

## Segurança

Ver `docs/SECURITY.md` — "Portal do agente". Contexto assinado, curta duração,
nonce anti-replay; o token identifica o dispositivo e não concede acesso às APIs
internas; "outro equipamento" vem de lista autorizada da unidade.

## Critérios de aceitação (quando a fase iniciar)

1. Funcionário abre chamado do PC atual sem digitar IP/patrimônio.
2. Funcionário abre chamado para outro PC informando o hostname do card/etiqueta.
3. Cada abertura cria uma conversa `agent_portal` independente.
4. O técnico responde pela mesma dashboard e a mensagem chega à página em tempo real.
5. Hostname inexistente/ambíguo segue para triagem, sem associação por aproximação.
6. Nenhuma identidade falsa de conversa é criada (sem telefone/JID fictício).
