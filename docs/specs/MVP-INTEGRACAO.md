# MVP — integração de suporte (GLPI + Tactical)

Escopo das **Fases 1–3**. Roda **sobre as conversas WhatsApp existentes**
`(session_id, chat_jid)`. Não cria `conversation_id` e **não** passa pelo Flow
Builder (ver `docs/DECISIONS.md`, D-010). O Flow Builder atual permanece
funcionando.

## Objetivo

Validar que o técnico consegue transformar uma conversa WhatsApp em ticket GLPI
associado ao hostname correto e consultar o contexto Tactical na mesma tela.

## Escopo do MVP

### 1. Painel lateral na conversa

- Exibir dados do solicitante e unidade.
- Criar ou visualizar ticket GLPI.
- Pesquisar e vincular equipamento (`device_binding`).
- Exibir status Tactical somente leitura.
- Abrir GLPI e Tactical em nova aba por link seguro.
- Deve ser um domínio de suporte **separado** da conversa genérica: não misturar
  campos GLPI/Tactical no modelo de chat existente.

### 2. Criação do ticket

Campos mínimos: título, descrição, categoria, localização, prioridade,
solicitante (ou identificação textual provisória), ativo/equipamento quando
confirmado, origem `WACalls`.

Havendo correspondência exata do hostname informado/recebido, o GLPI recebe o
`Computer ID` correspondente e associa o chamado ao ativo correto. A criação é
idempotente (não duplica ticket ao repetir a requisição).

### 3. Vínculo de equipamento

- Sugerir por hostname e localização.
- Mostrar secretaria, unidade e setor derivados/validados pelo Tactical e pelo
  padrão de hostname.
- Exigir confirmação do técnico em casos ambíguos.
- Persistir IDs GLPI/Tactical.
- Permitir trocar o equipamento com registro de auditoria.

### 4. Estados de integração

```text
not_linked
pending
linked
sync_error
```

O erro deve ser visível e permitir nova tentativa segura.

## Fora do MVP

- Enviar cada mensagem como acompanhamento GLPI.
- Resolver GLPI automaticamente ao finalizar conversa.
- Executar scripts, terminal, reboot ou comandos Tactical.
- Abrir chamado pelo agente Windows (isso é o Portal — Fase 4B).
- Criar `conversation_id` ou refatorar o Flow Builder.
- IA para classificação ou diagnóstico.
- Dashboards avançados.

## Critérios de aceitação

1. Técnico abre uma conversa WhatsApp e cria um ticket de homologação.
2. Repetir a mesma requisição não cria ticket duplicado.
3. Técnico seleciona um equipamento com vínculo confirmado.
4. Usuário informa hostname válido do card/etiqueta e o ticket é associado
   automaticamente ao ativo GLPI correspondente.
5. Hostname inexistente ou ambíguo não associa outro computador por aproximação
   e segue para triagem.
6. Painel mostra número/status do GLPI e online/offline do Tactical.
7. Indisponibilidade de uma API mostra erro recuperável sem perder a conversa.
8. Usuário sem permissão não cria, resolve ou troca vínculos.
9. Logs não contêm tokens nem dados sensíveis desnecessários.
10. Feature flag desativa o painel sem afetar o chat existente.

## Demonstração esperada

```text
Conversa WhatsApp selecionada
  -> Criar chamado
  -> Preencher dados
  -> Selecionar SDE-ARS-RCP-02
  -> GLPI #2841 criado e associado ao ativo
  -> Tactical: online
  -> Abrir sistema externo
```

## Perguntas que o planejamento (T-002) deve responder

- Como a conversa e o contato são identificados hoje? (resp.: `(session_id,
  chat_jid)`; ver `docs/ARCHITECTURE.md`.)
- Onde persistir vínculos sem acoplar o domínio de chat às APIs externas?
- Qual autenticação e endpoint GLPI serão usados?
- A API Tactical oferece link/token seguro de acesso remoto ou apenas URL do painel?
- Como feature flags são implementadas hoje no código?
- Existe fila/worker para retries ou será necessário um mecanismo mínimo?
