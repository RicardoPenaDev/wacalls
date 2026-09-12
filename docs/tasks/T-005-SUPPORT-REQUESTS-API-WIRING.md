# T-005 — support_requests, API interna e wiring GLPI/Tactical

Status: **especificação pronta para revisão; implementação não iniciada**.

Esta tarefa planeja somente o backend do domínio de suporte sobre as conversas
WhatsApp atuais `(session_id, chat_jid)`. T-003 (`device_bindings`) e T-004
(clientes GLPI/Tactical) estão concluídas. Não existe `conversation_id` nesta
fase.

> Testes da futura implementação serão 100% offline. Nenhuma credencial, token,
> senha, API key, body externo bruto ou chamada real entra em código, fixture,
> log, documentação ou teste.

## 1. Resultado esperado

Entregar, atrás de `WACALLS_SUPPORT_ENABLED`:

1. store aditivo `support_requests`, com isolamento por empresa SaaS e auditoria;
2. criação síncrona e idempotente de ticket GLPI;
3. resolução exata e opcional de equipamento usando `device_bindings`, GLPI e
   Tactical somente leitura;
4. endpoints internos aditivos para contexto, equipamento, criação e estado;
5. wiring das interfaces consumidoras pequenas no `cmd/server`;
6. tratamento conservador de resultado remoto ambíguo, sem worker automático.

T-005 não entrega frontend, SupportPanel, portal Windows, cache Tactical, runner
de retry nem sincronização posterior com o GLPI.

## 2. Inventário confirmado no código

- Stores seguem `type xStore struct{ db *sql.DB }` e `newXStore(ctx, db)`, criando
  schema aditivo no boot (`server.go`).
- `deviceBindingStore` já oferece `Get`, `Search`, `FindByHostname` e `Upsert`.
  `Get` recebe apenas `id`; todo consumidor HTTP deve comparar `TenantID` antes
  de devolver ou alterar o vínculo.
- Rotas são agregadas em `server.routes()` por `s.registerXRoutes(mux)` e usam
  padrões do `http.ServeMux` com `{param}`.
- `requireAuth` injeta `currentUser`; `currentUser.TenantID()` resolve a empresa
  SaaS. `sessionByID` oculta existência não autorizada com `404`.
- A conversa continua sendo `(session_id, chat_jid)`. Além de `sessionByID`, o
  handler deve confirmar a existência do chat com `chatMeta.Get(sid, jid)`.
- JSON usa `writeJSON`; request bodies relevantes devem usar `MaxBytesReader` ou
  `io.LimitReader` antes do decode.
- Cookies são `HttpOnly`, `SameSite=Lax`; o CORS atual é same-origin por padrão.
- O `Broker` já filtra eventos por dono/tenant, mas T-005 não precisa criar evento
  SSE: o endpoint de estado é a fonte de verdade. T-006 pode refazer a consulta
  após mutações.
- Workers existentes recebem o contexto do servidor e encerram em `ctx.Done()`.
  T-005 não inicia worker.
- Não há testes de handler com `httptest.NewRecorder/NewRequest` no backend
  atual; T-005 introduzirá esse padrão somente para as rotas novas.
- `internal/glpi.Client` implementa `FindComputerByHostname`, `GetComputer` e
  `CreateTicket`; o POST nunca é repetido automaticamente.
- `internal/tactical.Client` implementa `FindAgentByHostname` e `GetAgent`, ambos
  somente leitura.
- `.env.example` já contém `WACALLS_SUPPORT_ENABLED` e os nomes atuais de GLPI e
  Tactical. T-005 não precisa adicionar variável.

## 3. Divergências documentais e decisão aplicada

1. `docs/ARCHITECTURE.md` ainda mostra o modelo-alvo futuro com
   `conversation_id` e adverte contra confundir secretaria com tenant. D-013 e o
   código T-003 são mais específicos para o MVP: `owner_id`/`tenant_id` representam
   empresa SaaS; secretaria continua sendo Client/Site do Tactical.
2. T-002 cita `WACALLS_GLPI_TOKEN`/`WACALLS_TACTICAL_TOKEN`; T-004 substituiu
   esses nomes por OAuth2 GLPI e `WACALLS_TACTICAL_API_KEY`.
3. T-002/D-014 propunham retry por ticker. O cliente T-004 não oferece busca de
   ticket por `external_id`, e um timeout após POST pode esconder ticket criado.
   T-005 não terá worker; resultado ambíguo exige reconciliação manual. Esta
   decisão material é registrada em D-016.
4. O MVP descreve vínculo Ticket↔Computer, mas a API GLPI v2.3 não publica essa
   rota. T-005 envia hostname e Computer ID apenas como contexto textual.
5. O código expõe recursos do plano por `activePlanLimits`, mas
   `GET /api/settings/options` hoje só devolve o JSON persistido de `options`.
   A implementação deve acrescentar apenas `features.support: bool`, sem expor
   configuração ou segredos.
6. O CORS atual não permite o header `Idempotency-Key`; a implementação deve
   adicioná-lo a `Access-Control-Allow-Headers` sem abrir novas origens.
7. Alguns stores existentes usam `ON CONFLICT`, embora o projeto também abra
   MariaDB. O store novo usará `INSERT` + detecção de unique violation + `SELECT`
   e `UPDATE ... WHERE sync_state IN (...)`, evitando sintaxe de upsert exclusiva
   do SQLite.

## 4. Modelo `support_requests`

### 4.1 Schema lógico completo

O store seleciona DDL por driver. Não usar `AUTOINCREMENT`, `INSERT OR IGNORE`,
`ON CONFLICT`, `INSERT IGNORE` ou `ON DUPLICATE KEY` no caminho compartilhado.
IDs opacos são gerados no servidor; timestamps são segundos Unix UTC.

| Campo | SQLite | MariaDB/InnoDB | Nullable | Contrato |
|---|---|---|---|---|
| `id` | `TEXT` | `VARCHAR(36) CHARACTER SET ascii COLLATE ascii_bin` | não | UUID opaco |
| `owner_id` | `TEXT` | `VARCHAR(128) COLLATE utf8mb4_bin` | não | usuário criador |
| `tenant_id` | `TEXT` | `VARCHAR(128) COLLATE utf8mb4_bin` | não | empresa SaaS autenticada |
| `session_id` | `TEXT` | `VARCHAR(128) COLLATE utf8mb4_bin` | não | sessão autorizada |
| `chat_jid` | `TEXT` | `VARCHAR(255) COLLATE utf8mb4_bin` | não | chat autorizado |
| `device_binding_id` | `TEXT` | `VARCHAR(128) COLLATE utf8mb4_bin` | sim | vínculo local atual |
| `hostname_informed` | `TEXT` | `VARCHAR(64)` | não | seleção local atual, trimada |
| `hostname_normalized` | `TEXT` | `VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin` | não | hostname atual normalizado |
| `ticket_device_binding_id` | `TEXT` | `VARCHAR(128) COLLATE utf8mb4_bin` | sim | snapshot enviado na tentativa |
| `ticket_hostname_informed` | `TEXT` | `VARCHAR(64)` | sim | snapshot textual enviado ao GLPI |
| `ticket_hostname_normalized` | `TEXT` | `VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin` | sim | snapshot normalizado da tentativa |
| `ticket_glpi_computer_id` | `TEXT` | `VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin` | sim | Computer ID incluído no texto |
| `requester_name` | `TEXT` | `VARCHAR(120)` | não | texto simples |
| `title` | `TEXT` | `VARCHAR(200)` | não | texto simples |
| `description` | `TEXT` | `TEXT` | não | texto simples, até 8.000 bytes |
| `category_id` | `TEXT` | `VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin` | sim | ID decimal positivo |
| `location_id` | `TEXT` | `VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin` | sim | ID decimal positivo |
| `priority` | `INTEGER` | `SMALLINT UNSIGNED` | não | `0..6`; zero omite no GLPI |
| `glpi_ticket_id` | `TEXT` | `VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin` | sim | somente após `201` válido |
| `glpi_ticket_href` | `TEXT` | `VARCHAR(512)` | sim | href relativa validada |
| `external_id` | `TEXT` | `VARCHAR(40) CHARACTER SET ascii COLLATE ascii_bin` | não | formato exato da seção 7 |
| `idempotency_key` | `TEXT` | `VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin` | não | 16–128 bytes ASCII |
| `payload_fingerprint` | `TEXT` | `CHAR(64) CHARACTER SET ascii COLLATE ascii_bin` | não | SHA-256 hexadecimal |
| `sync_state` | `TEXT` | `VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin` | não | conjunto fechado da seção 6 |
| `last_error_code` | `TEXT` | `VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin` | não | código interno allowlisted |
| `attempt_count` | `INTEGER` | `INT UNSIGNED` | não | claims autorizados destinados a `CreateTicket` |
| `processing_token` | `TEXT` | `VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin` | não | vazio ou 128 bits em hex |
| `processing_started_at` | `INTEGER` | `BIGINT` | não | início do claim atual; zero fora dele |
| `processed_at` | `INTEGER` | `BIGINT` | não | fim da última tentativa; zero até ocorrer |
| `created_at` | `INTEGER` | `BIGINT` | não | criação |
| `updated_at` | `INTEGER` | `BIGINT` | não | última alteração |
| `synced_at` | `INTEGER` | `BIGINT` | não | sucesso; zero até `synced` |

No SQLite, tamanhos e conjunto de estados são `CHECK` constraints equivalentes
aos limites acima. No MariaDB, a tabela usa `ENGINE=InnoDB`,
`DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`; campos opacos usam `ascii_bin`.
Strings vazias são permitidas apenas nos campos não-null cujo contrato declara
zero/vazio. Os campos marcados nullable usam `NULL`, não sentinela vazia.

Constraints e índices obrigatórios, com nomes iguais nos dois drivers:

```text
PRIMARY KEY (id)
UNIQUE KEY uq_support_requests_tenant_idempotency
    (tenant_id, idempotency_key)
UNIQUE KEY uq_support_requests_tenant_external
    (tenant_id, external_id)
INDEX idx_support_requests_tenant_conversation
    (tenant_id, session_id, chat_jid, created_at)
INDEX idx_support_requests_tenant_glpi_ticket
    (tenant_id, glpi_ticket_id)
INDEX idx_support_requests_tenant_state
    (tenant_id, sync_state, updated_at)
```

O `CREATE TABLE IF NOT EXISTS` de cada driver inclui as constraints e índices;
migração futura usa catálogo do banco, nunca ignora erro de DDL. O construtor
pode rodar repetidamente sem perder dados. Não há foreign key nova: referências
a session/chat/binding são validadas por tenant na mesma operação transacional.

### 4.2 Semântica e snapshot de equipamento

`device_binding_id` e `hostname_*` descrevem a seleção local atual.
`ticket_device_binding_id`, `ticket_hostname_*` e `ticket_glpi_computer_id`
formam o snapshot mínimo usado para montar `TicketInput.Content`. O claim grava
esse snapshot na mesma transação que passa a linha para `processing`.

Depois que `CreateTicket` é chamado, o snapshot é imutável, inclusive em
`synced`, `unknown` ou `failed`. Uma falha comprovadamente anterior à chamada
pode voltar a `retryable_error`; o próximo claim autorizado pode substituir o
snapshot porque nenhuma tentativa de criação ocorreu. Trocar o vínculo atual
depois da chamada nunca reescreve o snapshot nem o ticket remoto.

`device_binding_id` representa o equipamento **afetado**. A origem não é
persistida nesta fase porque o canal é WhatsApp e não existe portal/agente
fornecendo um dispositivo de origem confiável. Fase 4B poderá adicionar
`source_device_id` sem alterar o contrato do equipamento afetado.

### 4.3 Dados proibidos

Não persistir em request, snapshot, auditoria ou logs:

- access/refresh tokens, senha, client secret ou API key;
- headers `Authorization`/`X-API-KEY`;
- response body bruto ou URL externa com query/userinfo;
- erro externo bruto;
- `custom_fields`, IP público, WMI, serviços, políticas, hardware detalhado ou
  outros campos Tactical não consumidos;
- conteúdo integral da conversa. Apenas a descrição explicitamente submetida
  para o chamado entra na solicitação.

## 5. Auditoria aditiva e transacional

`support_request_events` também tem DDL específico por driver:

| Campo | SQLite | MariaDB/InnoDB | Nullable |
|---|---|---|---|
| `id` | `TEXT PRIMARY KEY` | `VARCHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY` | não |
| `support_request_id` | `TEXT` | `VARCHAR(36) CHARACTER SET ascii COLLATE ascii_bin` | não |
| `tenant_id` | `TEXT` | `VARCHAR(128) COLLATE utf8mb4_bin` | não |
| `actor_type` | `TEXT` | `VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin` | não |
| `actor_user_id` | `TEXT` | `VARCHAR(128) COLLATE utf8mb4_bin` | sim |
| `action` | `TEXT` | `VARCHAR(48) CHARACTER SET ascii COLLATE ascii_bin` | não |
| `previous_device_id` | `TEXT` | `VARCHAR(128) COLLATE utf8mb4_bin` | sim |
| `device_binding_id` | `TEXT` | `VARCHAR(128) COLLATE utf8mb4_bin` | sim |
| `result_code` | `TEXT` | `VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin` | não |
| `created_at` | `INTEGER` | `BIGINT` | não |

Índice obrigatório:

```text
INDEX idx_support_request_events_request
    (tenant_id, support_request_id, created_at)
```

`actor_type` é `user` ou `system`. Toda ação HTTP grava
`actor_user_id=currentUser.ID`; somente recuperação automática usa `system` e
`actor_user_id=NULL`. `action` pertence ao conjunto
`created|ticket_claimed|ticket_synced|ticket_failed|ticket_retryable|
ticket_unknown|device_linked|device_replaced|processing_recovered_unknown|
reconciled_synced|reconciled_retryable`.

Eventos são append-only: o store não expõe update/delete. A criação da
`support_request` e `created` ocorre na mesma transação. Cada mudança de estado,
snapshot ou equipamento e seu evento ocorre em uma única transação; falha do
evento causa rollback da mudança. O evento guarda somente IDs mínimos e códigos
allowlisted: nunca descrição, credencial, body/resposta, href sensível ou erro
externo bruto.

## 6. Máquina de estados

Estados pequenos e explícitos:

| Estado | Significado | Nova tentativa |
|---|---|---|
| `new` | solicitação e evento inicial persistidos; nenhum claim ativo | somente fluxo inicial vencedor |
| `processing` | um token ganhou o claim e `CreateTicket` pode ter sido chamado | proibida por concorrentes |
| `synced` | GLPI confirmou `201`; ID/href persistidos | terminal |
| `retryable_error` | há prova de que nenhum ticket foi criado | somente ação explícita autorizada |
| `unknown` | não é possível provar se o GLPI criou o ticket | somente reconciliação autorizada |
| `failed` | validação/configuração/rejeição determinística | terminal; nova intenção usa nova chave |

Transições permitidas:

```text
new             -> processing       (somente fluxo inicial vencedor)
new             -> failed           (configuração/validação determinística)
new             -> retryable_error  (falha segura anterior ao ticket)
retryable_error -> processing       (retry explícito)
processing      -> synced
processing      -> retryable_error
processing      -> unknown
processing      -> failed
processing      -> unknown          (recuperação de claim órfão)
unknown         -> synced            (reconciliação encontrou ticket)
unknown         -> retryable_error   (reconciliação confirmou ausência)
```

Qualquer outra transição retorna conflito. Mudança de estado e evento sempre
fazem commit ou rollback juntos.

### 6.1 Classificação de falhas GLPI

`CreateTicket chamado` é a fronteira conservadora denominada “tentativa de POST
iniciada”. Isso não afirma que socket, headers ou body chegaram ao servidor
GLPI; a camada não conhece esse fato.

- JSON/header/campo inválido rejeitado antes de aceitar a solicitação retorna
  `400/422` e não cria linha.
- Validação ou configuração determinística detectada depois de persistir a
  solicitação, mas antes de operação externa, termina em `failed`, sem
  `CreateTicket`.
- Falha de autenticação comprovadamente anterior ao request de ticket, indicada
  por erro tipado do cliente com `Op=="token"`, nunca é ambígua:
  credencial/configuração permanentemente rejeitada vira `failed`; timeout,
  indisponibilidade ou falha transitória de token vira `retryable_error`.
- Rate limit local, de token ou resposta HTTP `429` válida que prove rejeição
  antes da criação vira `retryable_error`; preservar `Retry-After`, mas não
  iniciar worker nem retry automático.
- Depois que `CreateTicket` é chamado, erro de transporte, timeout,
  cancelamento, redirect bloqueado, `5xx`, body excessivo ou resposta de sucesso
  inválida vira `unknown`, porque não há prova de ausência do ticket.
- Resposta HTTP válida `400`, `403` ou `409` devolvida por `CreateTicket` prova
  rejeição determinística e vira `failed`. Outros `4xx` válidos são igualmente
  não ambíguos: `401/429` podem ser `retryable_error` quando a causa for
  corrigível; `404/422` são `failed`.
- `unknown` nunca recebe claim normal, nunca repete automaticamente e só sai por
  reconciliação administrativa com `IsAdmin()`.

### 6.2 Recuperação explícita de `processing` órfão

#### Topologia factual e decisão do MVP

O WACalls atual não suporta múltiplas instâncias ativas: `docker-compose.yml`
declara um único container fixo; o app usa arquivo SQLite/volume local,
`storage.Open` limita o pool a uma conexão e rejeita MariaDB como backend do
servidor; broker, sessões e rate limits mantêm estado em memória. Não há leader
election, lease ou coordenação distribuída.

Logo, o MVP exige **uma única instância ativa do servidor por implantação**.
A recuperação automática no startup depende dessa restrição temporária
(D-017). T-B002 usa MariaDB apenas no harness de stores e não habilita
multi-instância do app. Não criar heartbeat distribuído nesta fase.

#### Timeout, cutoff e execução

Configuração futura do wiring:

| Variável | Default | Mínimo | Máximo | Uso |
|---|---:|---:|---:|---|
| `WACALLS_SUPPORT_CREATE_TIMEOUT_SECONDS` | 120 | 30 | 600 | deadline da operação completa |
| `WACALLS_SUPPORT_RECOVERY_MARGIN_SECONDS` | 30 | 5 | 300 | margem depois do deadline |

Valores ausentes usam defaults; inválidos ou fora do intervalo falham no startup
sem segredo. `createTimeout` envolve, por um único `context.WithTimeout`, toda a
resolução de equipamento, obtenção OAuth e chamada `CreateTicket`. Todos os
clientes recebem esse contexto.

```text
orphanAge = createTimeout + recoveryMargin
cutoff    = time.Now().UTC().Add(-orphanAge).Unix()
```

O CAS do claim grava `processing_started_at=time.Now().UTC().Unix()`. O startup,
depois do schema e antes de servir rotas de suporte, chama
`RecoverOrphanedProcessing(ctx, cutoff, systemActor)`. Leitura comum nunca
converte estado. A rota administrativa pode chamar a mesma rotina para um ID,
mas exige `IsAdmin()`.

Somente `processing_started_at <= cutoff` é candidato; nunca converter todo
`processing`. Para cada linha, a rotina abre transação e executa CAS com token:

```sql
UPDATE support_requests
   SET sync_state='unknown', processing_token='',
       processing_started_at=0, processed_at=?, updated_at=?,
       last_error_code='orphaned_processing'
 WHERE id=? AND tenant_id=? AND sync_state='processing'
   AND processing_token=? AND processing_started_at<=?;
```

Somente `RowsAffected()==1` insere `processing_recovered_unknown` na mesma
transação. Falha do evento faz rollback; zero linhas é no-op. A rotina é
idempotente, e finalização tardia com token antigo encontra zero linhas.

## 7. Idempotência e concorrência

### 7.1 Contrato do header e replay

`Idempotency-Key` é obrigatório em `POST .../support/ticket`:

- 16 a 128 bytes ASCII;
- caracteres permitidos: `A-Z a-z 0-9 . _ ~ : -`;
- espaços, controles, Unicode e múltiplos valores são rejeitados;
- escopo: `tenant_id`; dois tenants podem usar a mesma chave;
- a chave é opaca e não deve conter telefone, e-mail, hostname ou outro PII.

Mesma chave e fingerprint sempre carregam a mesma linha, sem claim implícito:

| Estado persistido | Replay da criação |
|---|---|
| `synced` | `200`, ticket existente |
| `new` | `202`, sem claim; somente o fluxo inicial vencedor pode avançar |
| `processing` | `202`, sem claim |
| `retryable_error` | `202`, sem retry; ação explícita `/retry` necessária |
| `unknown` | `202`, sem claim; somente reconciliação |
| `failed` | `200`, resultado terminal existente |

Para corrigir uma solicitação `failed`, o cliente envia nova intenção ao
endpoint de criação com **nova** `Idempotency-Key`; não existe transição
`failed->processing`. Mesma chave com fingerprint diferente retorna `409` em
qualquer estado.

### 7.2 Payload canônico

Antes do fingerprint:

- `session_id` e `chat_jid` vêm do path autorizado;
- `requester_name`, `title`, `hostname`, `device_binding_id`, `category_id` e
  `location_id` recebem `TrimSpace`;
- hostname passa por `normalizeHostname`;
- descrição converte CRLF/CR para LF e preserva o restante;
- IDs decimais são validados e serializados sem zeros à esquerda;
- o decoder mantém presença separada do valor para todos os opcionais.

Campo obrigatório vazio e opcional explicitamente vazio são inválidos, em vez
de colapsarem silenciosamente em ausência. Presença integra o fingerprint onde
altera a intenção. Serializar struct `v:2`, nunca `map`, nesta ordem:

```json
{"v":2,"tenantId":"...","sessionId":"...","chatJid":"...","deviceBindingId":{"present":false,"value":""},"hostname":{"present":true,"value":"SDE-ARS-RCP-02"},"requesterName":"...","title":"...","description":"...","categoryId":{"present":false,"value":""},"locationId":{"present":false,"value":""},"priority":{"present":true,"value":3}}
```

Participam todos os campos que mudam semanticamente o ticket: tenant, conversa,
equipamento solicitado, hostname normalizado, requester, título, descrição,
categoria, localização e prioridade. Resultado variável das consultas externas
não participa; ele fica no snapshot da tentativa.

`payload_fingerprint = lowercase_hex(SHA-256(UTF-8(JSON canônico)))`, sempre 64
caracteres ASCII.

Formato exato:

```text
external_id = "wacalls-" +
    first32(lowercase_hex(SHA-256(
        "support-request:v1\n" + tenant_id + "\n" + support_request.id)))
```

O resultado tem 40 caracteres ASCII. `support_request.id` é UUID aleatório;
chave de idempotência, tenant legível e PII nunca são enviados no
`external_id`.

### 7.3 Criação, claim e finalização transacionais

1. Autenticar; derivar tenant/ator/permissões do contexto; autorizar session/chat;
   validar limites; calcular fingerprint.
2. Abrir transação. Fazer `INSERT` simples em `new` e inserir evento `created`.
   Ambos devem ter sucesso antes do commit.
3. Em unique violation de `(tenant_id,idempotency_key)`, fazer rollback,
   carregar por tenant/chave e aplicar a tabela de replay. Nunca executar upsert.
4. Somente a chamada que inseriu a linha pode seguir automaticamente. Resolver
   equipamento e preparar snapshot; um replay de linha `new` não toma o claim.
5. Para o fluxo inicial ou `/retry` permitido, gerar **novo** token de 16 bytes
   com `crypto/rand`, codificar em 32 hex minúsculos e abrir nova transação.
6. Executar o CAS:

   ```sql
   UPDATE support_requests
      SET sync_state='processing', processing_token=?,
          processing_started_at=?, processed_at=0,
          attempt_count=attempt_count+1,
          ticket_device_binding_id=?, ticket_hostname_informed=?,
          ticket_hostname_normalized=?, ticket_glpi_computer_id=?,
          updated_at=?
    WHERE id=? AND tenant_id=? AND processing_token=''
      AND sync_state=?;
   ```

   O fluxo inicial passa `expected_state='new'`. O endpoint `/retry` passa
   exclusivamente `expected_state='retryable_error'`; jamais aceita `new`,
   `processing`, `unknown`, `synced` ou `failed`.

7. Somente `RowsAffected()==1` insere `ticket_claimed` e commita. Falha do
   evento faz rollback. Somente esse vencedor chama uma vez `CreateTicket`.
8. Classificar o resultado e abrir transação final. Atualizar para
   `synced|failed|retryable_error|unknown`, limpar token/início, preencher
   `processed_at` e, no sucesso, ticket/href/`synced_at`, sempre com:

   ```sql
   WHERE id=? AND tenant_id=? AND sync_state='processing'
     AND processing_token=?
   ```

9. Exigir `RowsAffected()==1`, inserir o evento final na mesma transação e
   commitar. Zero linhas impede processo/token antigo de concluir claim novo;
   falha do evento reverte a mudança.

O retry preserva `idempotency_key`, `external_id` e fingerprint. Chamadas
concorrentes geram tokens distintos, mas só um CAS retorna uma linha; somente
esse vencedor chama `CreateTicket`. Nenhum código chama `/retry` automaticamente.

`attempt_count` conta claims autorizados para chamar `CreateTicket`; crash entre
commit do claim e chamada pode supercontar, nunca subtrair nem provocar retry.

### 7.4 Compatibilidade SQLite/MariaDB

O algoritmo usa apenas `INSERT`, `SELECT`, `UPDATE ... WHERE`, transações e
`RowsAffected`, comuns aos dois drivers. Conflito é detectado pelo erro de
unique/duplicate key do driver, seguido de `SELECT`; não usar sintaxe de upsert.

Correção não depende de isolamento por phantom: unique constraints serializam a
criação e o CAS serializa o claim. Testar com transação de escrita padrão do
SQLite e InnoDB em `READ COMMITTED` ou mais forte. Commit/rollback, erro de
unique, bloqueio concorrente e `RowsAffected` devem produzir o mesmo contrato
nos dois bancos.

## 8. Resolução e vínculo de equipamento

### 8.1 Fluxo de criação

1. Revalidar `hostname` na fronteira HTTP, aplicar `TrimSpace` e
   `normalizeHostname`; não confiar em normalização do frontend.
2. Na transação inicial, persistir request/evento em `new` com a seleção atual.
3. Se não houver hostname nem `device_binding_id`, seguir sem equipamento.
4. Se houver binding, carregar por ID, exigir mesmo `tenant_id` e revalidar que
   seu hostname casa exatamente com o hostname informado.
5. Procurar `FindByHostname(tenant, hostname)`; sem binding confirmado, consultar
   Tactical por hostname exato e somente leitura.
6. Consultar GLPI por hostname exato e revalidar no domínio todos os hostnames
   retornados com `normalizeHostname`.
7. Enriquecer `device_bindings` por `Upsert`, sem apagar IDs existentes.
8. No claim, congelar `ticket_device_binding_id`, `ticket_hostname_*` e
   `ticket_glpi_computer_id`; montar o contexto GLPI a partir desse snapshot.
9. Somente o claim vencedor chama `CreateTicket`.
10. Persistir `glpi_ticket_id`/href somente após `201` válido.

### 8.2 Matriz de equipamento

| Situação | Comportamento |
|---|---|
| hostname inválido/vazio quando informado | `422 invalid_hostname`; nenhum request aceito/POST |
| conversa sem equipamento | ticket permitido; snapshot registra ausência |
| binding de outro tenant | `404`; nunca revelar existência |
| binding local confirmado | usar IDs somente após validar tenant e hostname |
| não encontrado em ambos | criar sem vínculo, texto “não localizado”; triagem |
| somente Tactical encontrado | upsert `missing_glpi`; hostname no contexto |
| somente GLPI encontrado | upsert `missing_tactical`; hostname e Computer ID no contexto |
| ambos únicos e coerentes | upsert `matched`; hostname e Computer ID no contexto |
| múltiplo/ambíguo | não escolher; binding `conflict`; ticket segue para triagem |
| Tactical `status=offline` | resultado válido; persistir IDs e retornar status |
| Tactical indisponível | best-effort; não bloquear se GLPI puder prosseguir |
| GLPI indisponível na busca | criar sem associação somente se a criação puder prosseguir |
| troca de equipamento | validar request/conversa/tenant; alterar só vínculo atual e auditar |

Tactical offline é dado de domínio; Tactical indisponível é falha de integração.
Nenhum caso executa script, reboot, terminal ou acesso remoto.

### 8.3 Contexto textual e imutabilidade

`description` permanece texto simples. Para `TicketInput.Content`, cada fragmento
passa por `html.EscapeString`; quebras de linha viram `<br>`. O template usa
exclusivamente o snapshot:

```text
Descrição: <texto escapado>
Equipamento informado: <ticket_hostname_informed escapado ou “não informado”>
GLPI Computer ID: <ticket_glpi_computer_id validado ou “não confirmado”>
Vínculo nativo: indisponível na API v2.3; contexto textual
Origem: WACalls
Referência: <external_id>
```

Nunca interpolar HTML bruto. O frontend futuro deve renderizar descrição como
texto, nunca HTML não sanitizado ou `dangerouslySetInnerHTML`.

Troca posterior altera apenas `device_binding_id`/`hostname_*` atuais e o evento;
não altera `ticket_*`, o fingerprint ou o ticket GLPI já tentado/criado.
Integração adicional por PATCH/followup fica para tarefa futura. A ausência de
vínculo nativo Ticket↔Computer permanece contrato de D-015.

## 9. Interfaces declaradas no consumidor

Em `cmd/server/support_integration.go`:

```go
type supportGLPIClient interface {
    FindComputerByHostname(context.Context, string) (glpi.Computer, error)
    GetComputer(context.Context, string) (glpi.Computer, error)
    CreateTicket(context.Context, glpi.TicketInput) (glpi.CreatedTicket, error)
}

type supportTacticalClient interface {
    FindAgentByHostname(context.Context, string) (tactical.Agent, error)
    GetAgent(context.Context, string) (tactical.Agent, error)
}
```

Não adicionar métodos a `internal/glpi`/`internal/tactical`. Não declarar
`LinkComputerToTicket`, ticket update, followup, cache ou ação Tactical.

## 10. Endpoints mínimos

Todos são rotas novas. As três rotas de chat de `messageapi.go` permanecem
inalteradas.

### 10.1 Contrato final de autorização

- Toda rota usa `requireAuth`.
- Atendente autenticado não precisa ser administrador para criar chamado,
  consultar contexto/request ou vincular/trocar equipamento.
- Acesso à conversa usa o padrão real `userCanAccessSession(currentUser, sid)`,
  seguido de `sessionByID` e `chatMeta.Get(sid,jid)`.
- Criar chamado exige acesso à conversa do path. Vincular/trocar exige acesso à
  conversa associada ao request e binding do mesmo tenant.
- `tenant_id`, `owner_id`, `actor_user_id`, executor e permissões vêm apenas de
  `currentUser`/contexto do servidor. Nenhum é aceito do body.
- `owner_id` é o `currentUser.ID` da criação; cada evento usa o usuário que
  executou aquela ação, que pode ser diferente do criador.
- Request/device de outro tenant retorna `404`. Recurso do mesmo tenant para
  conversa sem permissão retorna `403`, sem expor dados do recurso.
- Inspeção factual: `user_permissions` persiste strings, `currentUser.Permissions`
  é `[]string` e o endpoint admin aceita qualquer string não vazia. Porém nenhum
  handler backend aplica permissão granular, não existe helper equivalente a
  `HasPermission`, e o catálogo frontend não contém `support.reconcile`.
- Portanto `support.reconcile` **não é capacidade existente end-to-end**. No MVP,
  reconciliação e recovery administrativo exigem `currentUser.IsAdmin()`.
  Permissão granular de reconcile fica para evolução futura com enforcement
  backend e cadastro UI explícitos; T-005 não inventa esse caminho.
- ACL por Secretaria/Client/Site Tactical fica fora da T-005 e não bloqueia o
  MVP; o limite atual é tenant empresa + sessões atribuídas.

| Método e path | Permissão/tenant | Estados | Idempotência |
|---|---|---|---|
| `GET /api/sessions/{sid}/chats/{jid}/support` | conversa acessível | todos | leitura |
| `GET /api/support/devices?query=&limit=` | usuário autenticado; busca tenant-scoped | n/a | leitura |
| `GET /api/support/devices/{id}` | binding do tenant | n/a | leitura |
| `PUT /api/support/requests/{id}/device` | conversa acessível; request/binding do tenant | todos; não altera snapshot congelado | mesmo vínculo é no-op |
| `POST /api/sessions/{sid}/chats/{jid}/support/ticket` | conversa acessível | cria `new`; pode finalizar síncrono | `Idempotency-Key` |
| `GET /api/support/requests/{id}` | request do tenant e conversa acessível | todos | leitura |
| `POST /api/support/requests/{id}/retry` | atendente com conversa acessível; request do tenant | somente `retryable_error` | novo token/CAS; IDs preservados |
| `POST /api/support/requests/{id}/reconcile` | somente admin; request do tenant | `unknown`; `processing` órfão | ação explícita |

### 10.2 Formato comum e XSS

Sucesso:

```json
{"supportRequest":{...},"device":null,"glpiComputer":null,"tacticalAgent":null,"warnings":[]}
```

Erro público:

```json
{"error":{"code":"stable_safe_code","message":"mensagem segura","retryAfterSeconds":0}}
```

Código/mensagem são allowlisted e nunca contêm URL, body ou erro externo.
`retryAfterSeconds` só aparece quando positivo. Descrição e hostname são
retornados como texto; o backend nunca retorna HTML montado para renderização.
O frontend futuro deve usar text nodes, nunca HTML não sanitizado.

### 10.3 `GET .../{jid}/support`

- `sid` e `jid`: 1–128 e 1–255 bytes; validar acesso antes da consulta.
- Retorna até 20 requests do tenant/conversa e o mais recente em
  `currentSupportRequest`.
- Não chama GLPI/Tactical ao abrir a conversa.
- `200`; `401`; `403` para sessão do mesmo tenant sem acesso; `404` para
  session/chat ausente ou de outro tenant; `503` disabled.

### 10.4 `GET /api/support/devices`

- `query`: 0–64 bytes; `limit`: default 20, máximo 100.
- Usa `Search(currentUser.TenantID(), query)`; nunca aceita tenant por query.
- Retorna apenas DTO seguro do binding; nenhuma descrição/telemetria Tactical.
- `200`; `400`; `401`; `503` disabled.

### 10.5 `GET /api/support/devices/{id}`

- `id`: 1–128 ASCII.
- Após `Get`, exige `binding.TenantID == currentUser.TenantID()`; divergência ou
  ausência retorna `404`.
- Consulta externa best-effort usa hostname exato e DTOs seguros.
- Offline retorna `200`; indisponibilidade parcial `200` com warning;
  ambiguidade `409`; resposta inválida `502`; indisponibilidade total `503`.

### 10.6 `PUT /api/support/requests/{id}/device`

Body até 4 KiB:

```json
{"deviceBindingId":"..."}
```

- `id`/campo: 1–128 ASCII; rejeitar campos desconhecidos.
- Carregar request por `(tenant_id,id)`, autorizar sua session/chat e exigir
  binding no mesmo tenant; outro tenant/ausente retorna `404`.
- Atualizar somente `device_binding_id`, `hostname_informed/normalized` atuais e
  `updated_at`; `ticket_*` e fingerprint permanecem imutáveis após tentativa.
- Estado `processing|synced|unknown|failed` aceita troca local, mas responde
  `glpiContextUpdated:false`. Em `new`, só o fluxo inicial pode capturar o
  vínculo; em `retryable_error`, o retry explícito captura o novo snapshot.
- Alteração/evento são transacionais. Mesmo vínculo retorna `200` sem novo evento.
- `200`; `400`; `401`; `403`; `404`; `409` concorrência; `422`; `503`.

### 10.7 `POST .../support/ticket`

Header `Idempotency-Key` obrigatório; body até 16 KiB:

```json
{
  "requesterName":"Maria Silva",
  "title":"Impressora offline",
  "description":"A impressora não responde.",
  "deviceBindingId":"optional-id",
  "hostname":"SDE-ARS-RCP-02",
  "categoryId":"9",
  "locationId":"4",
  "priority":3
}
```

Limites: requester 120, title 200, description 8.000 e hostname 64 bytes; IDs
128 bytes, exceto IDs GLPI decimais com até 32. Campos desconhecidos são
rejeitados. Binding+hostname devem casar exatamente ou `422 device_mismatch`.

Nunca aceitar `tenant_id`, `owner_id`, `actor_user_id`, permissões, `session_id`,
`chat_jid`, `external_id`, fingerprint, estado ou timestamps no body.

- Nova solicitação pode retornar `201 synced`, `202 unknown|retryable_error` ou
  resposta terminal segura conforme seção 11.
- Replay segue exatamente a tabela 7.1 e nunca cria claim.
- `400`; `401`; `403`; `404`; `409`; `422`; `429`; `502`; `503`.
- Resultado ambíguo retorna `202` com o mesmo request em `unknown`; não recomenda
  retry.

### 10.8 `GET /api/support/requests/{id}`

- `id`: 1–36 ASCII.
- Busca por `(tenant_id,id)` e autoriza a session/chat associada.
- Retorna `200`, `401`, `403`, `404` ou `503`; nunca expõe erro não allowlisted.

### 10.9 `POST /api/support/requests/{id}/retry`

Body vazio ou `{}`, máximo 1 KiB. Atendente autorizado não precisa ser admin,
mas o handler carrega por `(tenant_id,id)` e exige acesso à session/chat do
request.

- Permitido **somente** quando `sync_state='retryable_error'`.
- `new`, `processing`, `unknown`, `synced` e `failed` retornam
  `409 state_conflict`; `unknown` nunca usa esta rota.
- Gera novo `processing_token` aleatório e executa CAS com estado esperado
  exatamente `retryable_error`.
- Não altera `Idempotency-Key`, `external_id` ou fingerprint.
- Somente `RowsAffected()==1` chama `CreateTicket`; concorrência permite um POST.
- O handler executa a tentativa no próprio request, sem worker, e responde `202`
  quando o claim foi aceito, incluindo o estado resultante atual.
- Claim perdedor/estado incompatível responde `409`; `401/403/404/429/502/503`
  seguem a matriz comum.
- Nenhum scheduler, startup, replay ou outra rota aciona retry automaticamente.
- `failed` só admite nova intenção com nova chave.

### 10.10 `POST /api/support/requests/{id}/reconcile`

Body até 4 KiB; exige `currentUser.IsAdmin()`:

```json
{"outcome":"synced","glpiTicketId":"77","glpiTicketHref":"/api.php/v2.3/Assistance/Ticket/77"}
{"outcome":"safe_to_retry"}
{"outcome":"processing_orphaned"}
```

- `synced`: somente de `unknown`, com ID/href validados.
- `safe_to_retry`: somente de `unknown`, após confirmação humana de ausência.
- `processing_orphaned`: somente `processing` anterior ao cutoff; chama a mesma
  rotina CAS da seção 6.2, sem contato externo.
- Request de outro tenant/ausente retorna `404`; falta da permissão retorna
  `403`; estado/token concorrente retorna `409`.
- Estado/evento são transacionais e registram `actor_user_id`.
- `200`; `400`; `401`; `403`; `404`; `409`; `422`; `503`.

## 11. Matriz de erros internos → HTTP

| Origem | Código público | HTTP | Estado |
|---|---|---:|---|
| flag desligada | `support_disabled` | 503 | sem mutação |
| body/header inválido | `invalid_request` | 400 | sem mutação |
| campo fora do limite | `validation_failed` | 422 | sem mutação |
| auth ausente | `unauthorized` | 401 | sem mutação |
| conversa do mesmo tenant sem acesso | `forbidden` | 403 | sem mutação |
| reconciliação sem permissão | `forbidden` | 403 | sem mutação |
| recurso ausente/outro tenant | `not_found` | 404 | sem vazamento |
| fingerprint diferente | `idempotency_key_reused` | 409 | linha original intacta |
| estado/claim incompatível | `state_conflict` | 409 | sem mutação |
| match múltiplo | `device_ambiguous` | 409 | sem vínculo automático |
| rate limit comprovadamente anterior à criação | `rate_limited` | 429 | `retryable_error` se persistido |
| config local inválida após persistência | `integration_config_invalid` | 503 | `failed`, sem `CreateTicket` |
| token transitório antes do ticket | `integration_unavailable` | 503 | `retryable_error` |
| token/config permanentemente rejeitado | `integration_auth_failed` | 502 | `failed`, sem request de ticket |
| `CreateTicket` retorna `400/403/409/404/422` válido | `ticket_rejected` | 502 | `failed` |
| `CreateTicket` retorna `401` válido e corrigível | `integration_auth_failed` | 502 | `retryable_error` |
| `CreateTicket` retorna `429` válido | `rate_limited` | 429 | `retryable_error` |
| leitura externa inválida | `integration_bad_response` | 502 | warning ou `failed` |
| indisponibilidade em leitura | `integration_unavailable` | 503 | warning seguro |
| transporte/timeout/cancelamento/`5xx`/sucesso inválido de `CreateTicket` | `ticket_result_unknown` | 202 | `unknown` |

Não repassar `err.Error()` de GLPI/Tactical. Resposta `4xx` só é tratada como
rejeição comprovada quando o cliente entrega status HTTP válido; falha ao ler ou
classificar a resposta depois de chamar `CreateTicket` permanece `unknown`.

## 12. Feature flag e configuração

- `WACALLS_SUPPORT_ENABLED` usa parsing já adotado em `licenseRequired`:
  `1|on|true|sim|yes` (case-insensitive); vazio/qualquer outro valor = false.
- Não criar feature flags separadas. GLPI é obrigatório quando suporte está
  habilitado; Tactical é opcional e read-only.
- `WACALLS_SUPPORT_CREATE_TIMEOUT_SECONDS`: default `120`, mínimo `30`, máximo
  `600`; cobre a operação inteira descrita na seção 6.2.
- `WACALLS_SUPPORT_RECOVERY_MARGIN_SECONDS`: default `30`, mínimo `5`, máximo
  `300`; compõe `orphanAge=createTimeout+recoveryMargin`.
- Ausência usa default; parse inválido ou valor fora do intervalo falha no
  startup com `ErrConfig` seguro.
- Flag false: schema local é criado para preservar dados; clientes externos não
  são construídos; rotas existem e respondem `503 support_disabled`.
- Flag true + GLPI incompleto/inválido: startup falha com o `ErrConfig` seguro da
  T-004, nomeando somente a variável.
- Tactical: ambos base URL e API key vazios = integração opcional desligada;
  apenas um deles preenchido/config inválida = startup falha.
- `GET /api/settings/options` acrescenta somente:

  ```json
  {"features":{"support":true}}
  ```

  O boolean nunca expõe URL, usuário, senha, token, entidade, profile ou detalhe
  de configuração.
- A implementação T-005 adicionará as duas variáveis temporais à documentação de
  ambiente; esta revisão de planejamento não altera `.env.example`.

## 13. Segurança

- Tenant, owner, actor e permissões derivam exclusivamente de `currentUser`.
- Session/chat usam `userCanAccessSession` + `sessionByID` + `chatMeta.Get`.
- Toda operação por request/binding consulta ou compara tenant e depois verifica
  acesso à conversa; outro tenant é sempre `404`.
- Atendente comum pode criar/vincular/trocar apenas em conversa autorizada.
  Reconciliação e recovery administrativo exigem `IsAdmin()` no MVP.
- Limites: key 128; requester 120; title 200; description 8.000; hostname 64;
  IDs locais 128; body de criação 16 KiB, retry 1 KiB e mutações 4 KiB.
- Conteúdo GLPI escapa HTML; conteúdo local/API permanece texto simples.
- Nenhum erro externo bruto, URL, body, segredo ou header entra em API/log/store.
- Rate limit de criação/retry: 10 novos claims por 60 segundos por
  `(tenant_id, actor_user_id)`. Replay de leitura não consome; claim explícito
  consome e retorna `Retry-After` ao exceder.
- Idempotência continua garantida no banco; rate limit não a substitui.
- Eventos append-only auditam criar, claim, resultado, vínculo, troca,
  recuperação e reconciliação com `actor_type`/`actor_user_id`.
- CSRF mantém cookie `SameSite=Lax`, CORS same-origin e allowlist explícita.
  Adicionar apenas `Idempotency-Key` aos headers; nunca origem `*` com credencial.
- Tactical continua estritamente read-only; nenhuma ação destrutiva entra em
  interface, rota ou teste.
- ACL por Secretaria/Client/Site fica explicitamente fora desta tarefa.

## 14. Arquivos previstos na implementação

Criar:

```text
cmd/server/supportstore.go
cmd/server/supportstore_test.go
cmd/server/support_integration.go
cmd/server/support_integration_test.go
cmd/server/supportapi.go
cmd/server/supportapi_test.go
```

Alterar aditivamente:

```text
cmd/server/server.go       # campos, store e clientes; sem worker
cmd/server/httpapi.go      # registerSupportRoutes + Idempotency-Key no CORS
cmd/server/settingsapi.go  # somente features.support bool
```

Não alterar `internal/glpi` ou `internal/tactical` salvo necessidade comprovada
por contrato ausente; esta especificação não identifica nenhuma.

## 15. Sequência de implementação futura

0. **Desbloquear antes de código T-005:** implementar e validar
   `T-B002-HARNESS-MARIADB-STORES.md`.
1. Depois do aceite da T-B002, fazer a revisão final deste contrato.
2. Implementar DDL específico SQLite/MariaDB e executar a mesma suíte de
   contrato nos dois bancos.
3. Implementar store: criação/evento transacionais, conflito, replay, claim CAS,
   finalização por token e recuperação auditada de órfãos.
4. Cobrir concorrência entre conexões/processos e rollback de auditoria.
5. Implementar interfaces/orquestrador com mocks; cobrir classificação GLPI e
   snapshot de equipamento.
6. Implementar handlers, autorização por conversa, reconcile/recovery admin,
   limites, IDOR, rate limit e auditoria.
7. Fazer wiring no boot e expor somente `features.support`.
8. Executar validação completa SQLite/MariaDB, build e suíte offline; registrar
   evidência na task/STATUS.

Sem runner de retry em qualquer passo.

## 16. Plano de testes obrigatório

Todos offline quanto a GLPI/Tactical/WhatsApp, determinísticos e sem credenciais
reais. MariaDB usa instância descartável exclusiva de teste.

### Contrato de store — executar igualmente em SQLite e MariaDB

- DDL sobe duas vezes, preserva dados, tipos, nullability, constraints e índices;
- CRUD e todas as transições permitidas/proibidas;
- criação de request e evento inicial é atômica;
- mudança de estado/snapshot/equipamento e evento é atômica;
- falha injetada ao inserir evento causa rollback integral;
- eventos são append-only e registram `actor_type`/`actor_user_id`;
- isolamento por tenant em request, conversa, ticket, binding e auditoria;
- dois tenants usam a mesma `Idempotency-Key` sem conflito;
- mesma chave/fingerprint retorna o mesmo ID; fingerprint diferente retorna 409;
- replay em cada estado produz exatamente a tabela 7.1;
- plain INSERT + unique violation + SELECT funciona nos dois drivers;
- CAS e `RowsAffected` têm o mesmo resultado nos dois drivers;
- múltiplos `sql.DB` e processos concorrentes produzem uma linha e um claim;
- token antigo não finaliza claim recuperado/novo;
- recuperação de órfão é idempotente, respeita cutoff e gera um evento;
- leitura comum nunca converte `processing`;
- snapshot do equipamento congela na tentativa e troca posterior não o altera.

### Integração/orquestração

- mocks compilam contra interfaces consumidoras;
- somente o claim vencedor executa o mock `CreateTicket`;
- concorrência nunca produz segundo POST;
- retry de `retryable_error` por atendente autorizado gera token novo e só o
  claim/CAS vencedor executa `CreateTicket`;
- token/config antes da operação classifica `failed|retryable_error` sem unknown;
- `400/403/409` HTTP válido de `CreateTicket` termina `failed`;
- rate limit seguro termina `retryable_error` e preserva `Retry-After`;
- transporte/timeout/cancelamento/5xx/sucesso inválido termina `unknown`;
- replay e retry explícito em `unknown` nunca executam segundo POST;
- `external_id` tem formato exato, determinístico e não sensível;
- fingerprint v2 cobre presença/normalização de todos os campos semânticos;
- device binding encontrado, ausente, conflitante e de outro tenant;
- Tactical encontrado, offline, indisponível e ambíguo;
- GLPI encontrado, ausente, indisponível e ambíguo;
- hostname exato, HTML escapado e snapshot textual conforme D-015;
- nenhuma chamada externa real.

### HTTP (`httptest.NewRequest` + `httptest.NewRecorder`)

- rotas novas sem alterar chat;
- request, device e conversa de outro tenant retornam `404`;
- usuário do mesmo tenant sem acesso à conversa recebe `403`;
- atendente comum cria ticket e pode retry de `retryable_error` na conversa
  autorizada;
- atendente comum vincula/troca binding do mesmo tenant;
- tenant/owner/actor/permissões enviados no body são rejeitados;
- usuário não-admin não reconcilia nem força recovery de `processing`;
- admin reconcilia `unknown` e recupera órfão anterior ao cutoff;
- retry aceito responde `202`; `new`, `processing`, `unknown`, `synced` e
  `failed` respondem `409` sem chamar GLPI;
- dois retries concorrentes produzem um CAS vencedor e um único POST;
- limites de body/campos/key, campo desconhecido e HTML hostil;
- criação `201`, replay por estado `200/202`, conflito `409`;
- matriz `400/401/403/404/409/422/429/502/503`;
- flag desligada retorna `503` e zero chamada aos mocks;
- settings expõe somente `features.support`;
- nenhum body, URL, erro ou segredo externo aparece na resposta.

### Concorrência obrigatória

Além de goroutines, iniciar processos de teste independentes contra o mesmo
arquivo SQLite e contra a mesma base MariaDB descartável. Sincronizar a largada
para disputar insert e claim; afirmar uma linha, um evento `ticket_claimed` e
uma chamada registrada pelo mock compartilhado.

## 17. Comandos de validação futura

```text
gofmt -l cmd/server/support*.go cmd/server/server.go cmd/server/httpapi.go cmd/server/settingsapi.go
go vet ./cmd/server/...
go test ./cmd/server/ -run 'Support.*SQLite' -count=1
go test ./cmd/server/ -run 'Support.*MariaDB' -count=1
go test ./cmd/server/ -run 'Support.*Concurrent|Support.*Recovery|Support.*Audit' -count=20
go build ./...
go test ./...
git diff --check
```

O comando MariaDB deve criar/limpar infraestrutura descartável; não aceita
homologação compartilhada nem segredo real. Se não puder ser executado localmente
e no CI, T-005 permanece bloqueada. Race detector continua condicionado a
CGO/gcc. Nunca chamar GLPI, Tactical ou WhatsApp real.

## 18. Critérios de aceite

1. DDL e suíte equivalente passam em SQLite e MariaDB; sem isso T-005 não termina.
2. Tipos, tamanhos, nullability, índices e uniques seguem a seção 4.
3. Request/evento e toda mudança/evento são atomicamente persistidos.
4. Toda criação exige chave válida e persiste antes de operação externa.
5. Replay segue o estado; nunca cria claim implícito.
6. Mesma chave/fingerprint retorna o mesmo request; diferente retorna `409`.
7. Concorrência entre processos/conexões permite uma linha, um claim e um POST.
8. Token antigo não finaliza claim recuperado ou substituído.
9. Falha pré-ticket classifica `failed|retryable_error`; ambígua vira `unknown`.
10. `unknown` nunca repete e só sai por reconciliação autorizada.
11. Recuperação de órfão é explícita, por cutoff, idempotente e auditada.
12. Atendente comum cria/vincula em conversa autorizada, sem acesso transversal.
13. Reconcile e recovery administrativo exigem `IsAdmin()` no MVP.
14. Tenant/owner/actor/permissões nunca vêm do body; IDOR é coberto.
15. Hostname casa por igualdade normalizada e snapshot não muda retroativamente.
16. GLPI recebe texto escapado; não há vínculo Ticket↔Computer nativo.
17. Tactical permanece somente leitura; indisponibilidade não quebra o chat.
18. Limites, rate limit, redaction, XSS, auditoria e flag são cobertos.
19. Rotas/modelo de chat, Flow Builder, portal e SSE permanecem inalterados.
20. Build/suítes passam offline e nenhuma chamada externa real ocorre.

## 19. Bloqueio, riscos e dúvidas restantes

### Contratos fechados nesta revisão

- Atendente comum cria, vincula e troca em conversa autorizada.
- Reconciliação/recovery usam `IsAdmin()`; `support.reconcile` não existe
  end-to-end e fica como evolução futura.
- ACL futura por Secretaria/Client/Site está fora do MVP.
- `device_binding_id` é o equipamento afetado; snapshot da tentativa é imutável.
- Tactical é opcional/read-only; GLPI é obrigatório com a flag ligada.
- Criação é síncrona; retry somente explícito; sem cache/worker/SSE.
- Resultado `CreateTicket` ambíguo exige reconciliação (D-016).

### Bloqueio obrigatório antes do código

A infraestrutura atual e os stores inspecionados têm testes automatizados apenas
em SQLite; não existe teste MariaDB. A T-005 está bloqueada por
`T-B002-HARNESS-MARIADB-STORES.md`. A ordem obrigatória é: aceite da T-B002,
revisão final deste contrato e somente então implementação T-005. Homologação
manual não substitui esse aceite.

### Dúvidas não bloqueantes para o backend mínimo

1. Existe busca GLPI operacional confiável por `external_id`? Sem ela,
   reconciliação continua manual; isso jamais autoriza retry automático.
2. Qual integração futura refletirá troca pós-criação no GLPI: PATCH, followup ou
   nenhuma? T-005 mantém somente estado/auditoria local.

## 20. Não tocar na T-005

- frontend, `client/`, `SupportPanel` ou `ChatsPage`;
- portal/agente Windows;
- `conversation_id` ou modelo atual de mensagens;
- `messageapi.go` e suas rotas/handlers;
- Flow Builder (`flowexec*.go`, `flowbridge.go`, `flowapi.go`, `flowstore.go`);
- scripts, terminal, reboot, remoto ou escrita Tactical;
- sincronização de mensagens/followups com GLPI;
- vínculo nativo Ticket↔Computer;
- worker automático, ticker de retry ou cache Tactical;
- refatoração ampla de stores/clientes existentes;
- qualquer chamada real a GLPI, Tactical ou WhatsApp.

## 21. Estado deste documento

Revisão documental concluída em 2026-09-12; ainda não aprovada para implementação
ou push. Nenhum Go, TypeScript, schema runtime ou env foi alterado. Próximo passo
exato: disponibilizar e validar o harness MariaDB automatizado; depois revisar e
aprovar este contrato antes de iniciar qualquer código T-005.
