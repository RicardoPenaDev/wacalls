# T-005 — support_requests, API interna e wiring GLPI/Tactical

Status: **especificação e plano de implementação finalizados; implementação não iniciada**.

Esta tarefa planeja somente o backend do domínio de suporte sobre as conversas
WhatsApp atuais `(session_id, chat_jid)`. T-003 (`device_bindings`), T-004
(clientes GLPI/Tactical) e T-B002 (harness MariaDB de stores) estão concluídas.
Não existe `conversation_id` nesta fase.

> Testes da futura implementação serão 100% offline. Nenhuma credencial, token,
> senha, API key, body externo bruto ou chamada real entra em código, fixture,
> log, documentação ou teste.

## 1. Resultado esperado

Entregar, atrás de `WACALLS_SUPPORT_ENABLED`:

1. store aditivo `support_requests`, com isolamento por empresa SaaS e auditoria;
2. criação síncrona, atômica e idempotente de ticket GLPI (create+claim na mesma transação, eliminando requests presas em `new`);
3. resolução exata e opcional de equipamento usando `device_bindings`, GLPI e
   Tactical somente leitura;
4. endpoints internos aditivos para contexto, equipamento, criação, retry, concorrência e reconciliação segura;
5. wiring das interfaces consumidoras pequenas no `cmd/server`, com dialeto explícito;
6. recuperação conservadora de claim órfão no boot (single-instance), sem worker automático.

T-005 não entrega frontend, SupportPanel, portal Windows, cache Tactical, runner
de retry nem rate limiting in-memory (postergado para hardening futuro).

## 2. Inventário confirmado no código

- Stores seguem `type xStore struct{ db *sql.DB }` e `newXStore(ctx, db)`, criando
  schema aditivo no boot (`server.go`). Atualmente nenhum store recebe informação
  de dialeto do banco; a T-005 introduz `SQLDialect` explícito (`sqlite` e `mariadb`)
  no construtor do novo store. O runtime de produção em `cmd/server` inicializa com
  `DialectSQLite`. Código de produção NUNCA deve importar `internal/testdb`; este pacote
  é restrito aos testes contratuais (`*_test.go`).
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
  atual; T-005 introduzirá esse padrão para as rotas novas.
- `internal/glpi.Client` implementa `FindComputerByHostname`, `GetComputer` e
  `CreateTicket`; a T-005 adiciona `GetTicket` para viabilizar reconciliação segura.
  O POST nunca é repetido automaticamente.
- `internal/tactical.Client` implementa `FindAgentByHostname` e `GetAgent`, ambos
  somente leitura.
- Não existe componente de rate limit reutilizável por tenant/usuário no backend
  (apenas `loginLimiter` por IP no login). A proposta de 10 claims/60s foi
  removida da T-005 e transferida para hardening posterior.
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
4. D-017 estabelece o requisito temporário de instância única (single-instance)
   no MVP, permitindo que a recuperação de órfãos (`RecoverOrphanedProcessing`)
   no startup seja segura sem coordenação distribuída.
5. D-018 define o padrão de create+claim atômico: a inserção inicial já nasce em
   `processing` com token e timestamps, gravando `created` e `ticket_claimed` na
   mesma transação. Não existe estado `new` persistido de forma órfã.
6. D-018 também define a reconciliação segura: `outcome=synced` exige verificação
   remota via `GetTicket` no GLPI validando o `external_id`, derivando ID e href
   diretamente da resposta do GLPI e rejeitando href arbitrário.
7. O MVP descreve vínculo Ticket↔Computer, mas a API GLPI v2.3 não publica essa
   rota. T-005 envia hostname e Computer ID apenas como contexto textual.
8. O código expõe recursos do plano por `activePlanLimits`, mas
   `GET /api/settings/options` hoje só devolve o JSON persistido de `options`.
   A implementação acrescentará apenas `features.support: bool`, sem expor
   configuração ou segredos.
9. O CORS atual não permite o header `Idempotency-Key`; a implementação deve
   adicioná-lo a `Access-Control-Allow-Headers` sem abrir novas origens.
10. O store novo selecionará DDL por `SQLDialect` explícito (`sqlite` ou `mariadb`)
    passado no construtor. Não usar detecção por probing ou reflexão. O runtime do
    WACalls inicializa com `DialectSQLite`; o harness MariaDB da T-B002 é usado
    exclusivamente pelos testes de contrato. Código de produção não deve importar
    `internal/testdb`. A T-005 não converte o runtime do servidor em MariaDB.

## 4. Modelo `support_requests`

### 4.1 Schema lógico completo

O store seleciona DDL por `SQLDialect` explícito (`sqlite` e `mariadb`) passado no construtor. Não usar `AUTOINCREMENT`, `INSERT OR IGNORE`, `ON CONFLICT`, `INSERT IGNORE` ou `ON DUPLICATE KEY` no caminho compartilhado. IDs opacos são gerados no servidor; timestamps são segundos Unix UTC.

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
| `sync_state` | `TEXT` | `VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin` | não | conjunto fechado: processing, synced, retryable_error, unknown, failed |
| `last_error_code` | `TEXT` | `VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin` | não | código interno allowlisted |
| `attempt_count` | `INTEGER` | `INT UNSIGNED` | não | claims autorizados destinados a `CreateTicket` |
| `processing_token` | `TEXT` | `VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin` | não | 128 bits em hex; vazio fora de processing |
| `processing_started_at` | `INTEGER` | `BIGINT` | não | início do claim atual em UTC; zero fora dele |
| `processed_at` | `INTEGER` | `BIGINT` | não | fim da última tentativa em UTC; zero até ocorrer |
| `created_at` | `INTEGER` | `BIGINT` | não | criação em UTC |
| `updated_at` | `INTEGER` | `BIGINT` | não | última alteração em UTC |
| `synced_at` | `INTEGER` | `BIGINT` | não | sucesso em UTC; zero até `synced` |

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
formam o snapshot mínimo usado para montar `TicketInput.Content`. Na criação, o
claim atômico grava imediatamente os dados locais disponíveis nesse snapshot.
Se consultas externas subsequentes resolverem IDs adicionais (ex.: GLPI Computer ID),
esse snapshot é enriquecido em transação separada antes de `CreateTicket`,
condicionada estritamente ao mesmo `processing_token`.

Depois que `CreateTicket` é chamado, o snapshot é imutável, inclusive em
`synced`, `unknown` ou `failed`. Uma falha comprovadamente anterior à chamada
pode voltar a `retryable_error`; o próximo claim autorizado de retry pode
substituir o snapshot porque nenhuma tentativa de criação ocorreu. Trocar o
vínculo atual (`PUT /device`) depois da chamada nunca reescreve o snapshot nem o
ticket remoto.

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

Eventos são append-only: o store não expõe update/delete. A criação inicial da
`support_request` (já com claim em `processing`) e os eventos `created` e
`ticket_claimed` ocorrem na mesma transação atômica. Cada mudança posterior de
estado, snapshot ou equipamento e seu evento correspondente ocorre em uma única
transação; falha do evento causa rollback da mudança. O evento guarda somente IDs
mínimos e códigos allowlisted: nunca descrição, credencial, body/resposta, href
sensível ou erro externo bruto.

## 6. Máquina de estados

Estados pequenos e explícitos:

| Estado | Significado | Nova tentativa |
|---|---|---|
| `processing` | solicitação criada e claim ativo; `CreateTicket` em andamento | proibida por concorrentes |
| `synced` | GLPI confirmou `201`; ID/href persistidos | terminal |
| `retryable_error` | há prova de que nenhum ticket foi criado | somente ação explícita `/retry` autorizada |
| `unknown` | não é possível provar se o GLPI criou o ticket | somente reconciliação administrativa autorizada |
| `failed` | validação/configuração/rejeição determinística | terminal; nova intenção usa nova chave |

> **Eliminação do estado `new` persistente:**
> `StateNew` **não existe como estado persistente** na tabela `support_requests`. A coluna
> `sync_state` admite estritamente o conjunto: `processing`, `synced`, `retryable_error`,
> `unknown` e `failed`.
> No domínio da aplicação, "novo" representa unicamente a intenção em memória recebida
> no request HTTP antes da persistência. Se o banco permitisse comitar uma linha em `new`
> separada do claim, uma queda de processo imediatamente posterior ao commit deixaria a
> requisição presa para sempre: o replay responderia `202` sem tomar claim, o `/retry`
> rejeitaria `new` (que só aceita `retryable_error`), e o startup ignoraria a linha
> (pois `RecoverOrphanedProcessing` busca apenas `processing`).
> Na criação atômica da T-005, o primeiro `INSERT` já grava a linha em `processing`
> (com `processing_token`, `processing_started_at` e eventos `created` + `ticket_claimed`
> na mesma transação atômica). Dessa forma, qualquer falha ou crash posterior do processo é
> 100% coberta pela recuperação de órfãos baseada em cutoff temporal.

Transições permitidas:

```text
(criação atômica) -> processing       (created + ticket_claimed na mesma transação)
processing        -> synced           (GLPI confirmou 201 com ID e href válidos)
processing        -> failed           (rejeição determinística 4xx do GLPI ou config inválida)
processing        -> retryable_error  (falha segura comprovadamente anterior ao CreateTicket)
processing        -> unknown          (falha ambígua pós-POST ou recuperação de órfão)
retryable_error   -> processing       (retry explícito via CAS com token novo)
unknown           -> synced           (reconciliação admin após GetTicket confirmar external_id)
unknown           -> retryable_error  (reconciliação admin confirmando ausência de ticket)
```

Qualquer outra transição retorna conflito. Mudança de estado e evento sempre
fazem commit ou rollback juntos.

### 6.1 Classificação de falhas GLPI

`CreateTicket chamado` é a fronteira conservadora denominada “tentativa de POST
iniciada”. Isso não afirma que socket, headers ou body chegaram ao servidor
GLPI; a camada não conhece esse fato.

- JSON/header/campo inválido rejeitado antes de aceitar a solicitação retorna
  `400/422` e não cria linha.
- Falha de autenticação comprovadamente anterior ao request de ticket, indicada
  por erro tipado do cliente com `Op=="token"`, nunca é ambígua:
  credencial/configuração permanentemente rejeitada vira `failed`; timeout,
  indisponibilidade ou falha transitória de token vira `retryable_error`.
- Resposta HTTP `429` do GLPI que prove rejeição antes ou durante criação vira
  `retryable_error`; preservar `Retry-After`, mas não iniciar worker nem retry automático.
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
| `processing` | `202`, sem claim |
| `retryable_error` | `202`, sem retry automático; ação explícita `/retry` necessária |
| `unknown` | `202`, sem claim; somente reconciliação |
| `failed` | `200`, resultado terminal existente |

Para corrigir uma solicitação `failed`, o cliente envia nova intenção ao
endpoint de criação com **nova** `Idempotency-Key`; não existe transição
`failed->processing`. Mesma chave com fingerprint diferente retorna `409` em
qualquer estado (`idempotency_key_reused`).

Em caso de requisições concorrentes disparadas com a mesma chave e mesmo fingerprint,
a corrida na inserção inicial é resolvida pelo banco: a segunda requisição sofre
violação de chave única, faz rollback, recarrega a linha e aplica a tabela de replay
acima (respondendo `202`), **nunca retornando `state_conflict`** para um replay válido.

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

### 7.3 Criação, claim e finalização transacionais (Create+Claim Atômico)

Para eliminar a janela em que um processo poderia cair entre o `INSERT` inicial
e o claim (deixando o registro preso em `new` para sempre), a criação da
`support_request` realiza a persistência e a tomada do claim na **mesma transação inicial**:

1. **Autenticação e Validação:** Autenticar; derivar tenant/ator/permissões do contexto;
   autorizar session/chat; validar limites de payload; calcular `payload_fingerprint` e `external_id`.
2. **Preparação de Token:** Gerar previamente o `processing_token` de 16 bytes criptográficos
   (32 caracteres hex minúsculos).
3. **Transação 1 (Criação + Claim Atômico):**
   - Abrir transação no banco.
   - Executar `INSERT` em `support_requests` já com `sync_state='processing'`,
     `processing_token`, `processing_started_at=time.Now().UTC().Unix()`, `attempt_count=1`,
     e snapshot local imediato (`ticket_device_binding_id`, `ticket_hostname_*`).
   - Inserir evento `created` na mesma transação.
   - Inserir evento `ticket_claimed` na mesma transação.
   - Commit!
   - Em caso de unique violation de `(tenant_id, idempotency_key)`, fazer rollback,
     carregar por tenant/chave e aplicar a tabela de replay da seção 7.1.
4. **Consultas Externas e Enriquecimento (Fora de Transação):**
   - Nenhuma chamada externa é executada dentro da Transação 1.
   - Executar resolução de equipamento em `device_bindings`, Tactical e GLPI (somente leitura,
     com context timeout).
   - Se dados remotos enriquecerem o snapshot (ex.: `glpi_computer_id`), abrir **Transação 2 (Enriquecimento)**:
     ```sql
     UPDATE support_requests
        SET ticket_glpi_computer_id=?, ticket_device_binding_id=?, updated_at=?
      WHERE id=? AND tenant_id=? AND sync_state='processing' AND processing_token=?;
     ```
     Se `RowsAffected()==0`, o claim foi perdido (ex.: cutoff de órfão assumido por outro fluxo); abortar imediatamente sem chamar `CreateTicket`.
     Se houver alteração material, registrar evento de auditoria `snapshot_enriched` na mesma transação.
     Commit!
5. **Chamada Remota GLPI (Zero Transações Abertas):**
   - Chamar `CreateTicket` no cliente GLPI fora de qualquer transação de banco de dados.
6. **Transação 3 (Finalização por CAS):**
   - Classificar o resultado conforme a seção 6.1 (`synced`, `failed`, `retryable_error` ou `unknown`).
   - Abrir transação e atualizar:
     ```sql
     UPDATE support_requests
        SET sync_state=?, last_error_code=?, glpi_ticket_id=?, glpi_ticket_href=?,
            synced_at=?, processing_token='', processing_started_at=0,
            processed_at=?, updated_at=?
      WHERE id=? AND tenant_id=? AND sync_state='processing' AND processing_token=?;
     ```
   - Se `RowsAffected()==1`, inserir o evento final correspondente (`ticket_synced`, `ticket_failed`,
     `ticket_retryable` ou `ticket_unknown`) na mesma transação e comitar.
   - Se `RowsAffected()==0`, o processo perdeu o claim para o timeout de órfãos; abortar rollback.

### 7.4 Disputa de Retry (`POST /api/support/requests/{id}/retry`)

- O endpoint `/retry` é restrito estritamente a solicitações em `retryable_error`.
- O estado `unknown` é **proibido** e nunca aceita retry (exige reconciliação administrativa).
- Para executar o retry, gera-se um novo `processing_token` e executa-se o CAS:
  ```sql
  UPDATE support_requests
     SET sync_state='processing', processing_token=?,
         processing_started_at=?, processed_at=0,
         attempt_count=attempt_count+1, updated_at=?
   WHERE id=? AND tenant_id=? AND sync_state='retryable_error' AND processing_token='';
  ```
- **Contrato de Disputa Concorrente:**
  - **Vencedor (`RowsAffected()==1`):** insere evento `ticket_claimed`, comita, executa `CreateTicket`
    fora de transação, finaliza via CAS e responde `202 Accepted` com os dados do request.
  - **Perdedor (`RowsAffected()==0`):** retorna **`409 Conflict`** (`state_conflict`).
  - O retry preserva `idempotency_key`, `external_id` e fingerprint originais.

### 7.5 Compatibilidade SQLite/MariaDB

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
2. Na transação inicial atômica (Passo 3 da seção 7.3), persistir o request já em
   `processing` com a seleção local disponível.
3. Se não houver hostname nem `device_binding_id`, seguir sem equipamento.
4. Se houver binding, carregar por ID, exigir mesmo `tenant_id` e revalidar que
   seu hostname casa exatamente com o hostname informado.
5. Procurar `FindByHostname(tenant, hostname)`; sem binding confirmado, consultar
   Tactical por hostname exato e somente leitura.
6. Consultar GLPI por hostname exato e revalidar no domínio todos os hostnames
   retornados com `normalizeHostname`.
7. Enriquecer `device_bindings` por `Upsert`, sem apagar IDs existentes.
8. Enriquecer o snapshot na Transação 2 condicionada ao `processing_token`.
9. Somente o claim vencedor chama `CreateTicket` fora de transação.
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

### 8.3 Contexto textual, imutabilidade e proteção XSS

`description` e `title` permanecem texto simples original tanto no banco de dados
quanto nas respostas JSON da API, sem entidades HTML (como `&lt;`), evitando dupla
codificação na renderização de texto do React. O frontend futuro é estritamente proibido
de utilizar `dangerouslySetInnerHTML`.

Para o campo `TicketInput.Content` enviado ao GLPI, cada fragmento textual controlado
pelo usuário passa individualmente por `html.EscapeString` no servidor, e somente após o
escape as quebras de linha `\n` são convertidas para `<br>`. O template estrutural é
gerado exclusivamente pelo servidor usando o snapshot:

```text
Descrição: <html.EscapeString(req.Description) com \n -> <br>>
Equipamento informado: <html.EscapeString(ticket_hostname_informed) ou “não informado”>
GLPI Computer ID: <ticket_glpi_computer_id ou “não confirmado”>
Vínculo nativo: indisponível na API v2.3; contexto textual
Origem: WACalls
Referência: <external_id>
```

Nunca interpolar HTML bruto. A troca posterior de equipamento (`PUT /device`) altera
apenas `device_binding_id` e `hostname_*` atuais e gera auditoria; ela nunca altera
`ticket_*` (snapshot congelado), o fingerprint ou o ticket GLPI já tentado/criado.
Durante o estado `processing`, `PUT /device` responde `200 OK` com `glpiContextUpdated:false`,
atualizando apenas a seleção local atual sem concorrer com o snapshot da tentativa em andamento.

## 9. Interfaces declaradas no consumidor

Em `cmd/server/support_integration.go`:

```go
type supportGLPIClient interface {
    FindComputerByHostname(context.Context, string) (glpi.Computer, error)
    GetComputer(context.Context, string) (glpi.Computer, error)
    CreateTicket(context.Context, glpi.TicketInput) (glpi.CreatedTicket, error)
    GetTicket(context.Context, string) (glpi.Ticket, error)
}

type supportTacticalClient interface {
    FindAgentByHostname(context.Context, string) (tactical.Agent, error)
    GetAgent(context.Context, string) (tactical.Agent, error)
}
```

O método `GetTicket` é adicionado ao `internal/glpi.Client` exclusivamente para
viabilizar a reconciliação segura por `external_id`. Não declarar `LinkComputerToTicket`,
ticket update, followup, cache ou ação destrutiva Tactical.

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
- Reconciliação e recovery administrativo exigem `currentUser.IsAdmin()` no MVP.
- ACL por Secretaria/Client/Site Tactical fica fora da T-005 e não bloqueia o
  MVP; o limite atual é tenant empresa + sessões atribuídas.

| Método e path | Permissão/tenant | Estados | Idempotência |
|---|---|---|---|
| `GET /api/sessions/{sid}/chats/{jid}/support` | conversa acessível | todos | leitura |
| `GET /api/support/devices?query=&limit=` | usuário autenticado; busca tenant-scoped | n/a | leitura |
| `GET /api/support/devices/{id}` | binding do tenant | n/a | leitura |
| `PUT /api/support/requests/{id}/device` | conversa acessível; request/binding do tenant | todos; não altera snapshot congelado | mesmo vínculo é no-op |
| `POST /api/sessions/{sid}/chats/{jid}/support/ticket` | conversa acessível | cria `processing`; pode finalizar síncrono | `Idempotency-Key` |
| `GET /api/support/requests/{id}` | request do tenant e conversa acessível | todos | leitura |
| `POST /api/support/requests/{id}/retry` | atendente com conversa acessível; request do tenant | somente `retryable_error` | novo token/CAS; IDs preservados |
| `POST /api/support/requests/{id}/reconcile` | somente admin; request do tenant | `unknown`; `processing` órfão | ação explícita |

### 10.2 Formato comum e respostas públicas

Sucesso:

```json
{"supportRequest":{...},"device":null,"glpiComputer":null,"tacticalAgent":null,"warnings":[]}
```

Erro público:

```json
{"error":{"code":"stable_safe_code","message":"mensagem segura","retryAfterSeconds":0}}
```

Código/mensagem são allowlisted e nunca contêm URL, body ou erro externo.
`retryAfterSeconds` só aparece quando positivo (ex.: 429 retornado pelo GLPI).
Descrição e hostname são retornados como texto simples puro; o backend nunca
retorna entidades HTML escapadas no JSON. O frontend deve renderizar como text nodes.

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
- Estado `processing|synced|unknown|failed` aceita troca do vínculo local, mas responde
  `200 OK` com `glpiContextUpdated: false`. Em `processing`, a alteração atualiza apenas a
  seleção local atual e não concorre com a Transação 2 (que exige `processing_token` para
  tocar em `ticket_*`), garantindo que o snapshot congelado da tentativa em andamento não seja
  sobrescrito. Em `retryable_error`, uma troca posterior será capturada pelo próximo claim do retry.
- Alteração/evento são transacionais (gravando `device_linked` ou `device_replaced`).
  Mesmo vínculo atual retorna `200 OK` sem novo evento.
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

- Nova solicitação executa criação + claim atômico e pode retornar `201 synced`,
  `202 unknown|retryable_error` ou resposta terminal segura conforme seção 11.
- Replay segue exatamente a tabela 7.1 e nunca cria novo claim.
- `400`; `401`; `403`; `404`; `409`; `422`; `502`; `503`.

### 10.8 `GET /api/support/requests/{id}`

- `id`: 1–36 ASCII.
- Busca por `(tenant_id,id)` e autoriza a session/chat associada.
- Retorna `200`, `401`, `403`, `404` ou `503`; nunca expõe erro não allowlisted.

### 10.9 `POST /api/support/requests/{id}/retry`

Body vazio ou `{}`, máximo 1 KiB. Atendente autorizado não precisa ser admin,
mas o handler carrega por `(tenant_id,id)` e exige acesso à session/chat do
request.

- Permitido **somente** quando `sync_state='retryable_error'`.
- `processing`, `unknown`, `synced` e `failed` retornam `409 state_conflict`;
  `unknown` nunca aceita esta rota.
- Gera novo `processing_token` aleatório e executa CAS com estado esperado
  exatamente `retryable_error`.
- Não altera `Idempotency-Key`, `external_id` ou fingerprint.
- **Disputa concorrente:** Vencedor do CAS (`RowsAffected()==1`) chama `CreateTicket`
  fora de transação e responde `202`; perdedor do CAS (`RowsAffected()==0`)
  responde `409 state_conflict`.
- Nenhum scheduler, startup, replay ou outra rota aciona retry automaticamente.
- `failed` só admite nova intenção com nova chave.

### 10.10 `POST /api/support/requests/{id}/reconcile`

Body até 4 KiB; exige `currentUser.IsAdmin()`:

```json
{"outcome":"synced","glpiTicketId":"77"}
{"outcome":"safe_to_retry"}
{"outcome":"processing_orphaned"}
```

- **`outcome == "synced"`:**
  - Somente a partir de `unknown`.
  - O backend **não confia** em href do body. Ele invoca `GetTicket(ctx, glpiTicketId)`
    no GLPI (fora de transação).
  - Se o ticket não existir no GLPI, retorna `404 not_found`.
  - Confirma se `ticket.ExternalID == request.ExternalID`. Em caso de divergência,
    retorna `422 validation_failed` (`reconcile_external_id_mismatch`).
  - Obtendo confirmação exata, deriva `id` e `href` diretamente da resposta do GLPI.
  - Executa CAS para transitar `unknown -> synced`, grava `reconciled_synced` e comita.
- **`outcome == "safe_to_retry"`:**
  - Somente a partir de `unknown`, após confirmação humana de ausência.
  - Executa CAS para transitar `unknown -> retryable_error`, grava `reconciled_retryable` e comita.
- **`outcome == "processing_orphaned"`:**
  - Somente para solicitações em `processing` com `processing_started_at <= cutoff`.
  - Não aceita ticket ID. Executa CAS para `unknown` e grava `processing_recovered_unknown`.
  - Se a solicitação não atender ao cutoff ou o token/estado tiver mudado, retorna `409 state_conflict`.
- Request de outro tenant/ausente retorna `404`; falta da permissão admin retorna `403`.
- `200`; `400`; `401`; `403`; `404`; `409`; `422`; `503`.

## 11. Matriz de erros internos → HTTP

| Origem | Código público | HTTP | Estado |
|---|---|---:|---|
| flag desligada | `support_disabled` | 503 | sem mutação |
| body/header inválido | `invalid_request` | 400 | sem mutação |
| campo fora do limite | `validation_failed` | 422 | sem mutação |
| auth ausente | `unauthorized` | 401 | sem mutação |
| conversa do mesmo tenant sem acesso | `forbidden` | 403 | sem mutação |
| reconciliação sem permissão admin | `forbidden` | 403 | sem mutação |
| recurso ausente/outro tenant | `not_found` | 404 | sem vazamento |
| ticket GLPI inexistente na reconciliação | `ticket_not_found` | 404 | mantém `unknown` |
| external_id divergente no ticket GLPI | `reconcile_external_id_mismatch` | 422 | mantém `unknown` |
| fingerprint diferente na mesma chave | `idempotency_key_reused` | 409 | linha original intacta |
| estado/claim incompatível (ou disputa de retry) | `state_conflict` | 409 | sem mutação |
| match múltiplo em equipamento | `device_ambiguous` | 409 | sem vínculo automático |
| config local inválida após persistência | `integration_config_invalid` | 503 | `failed`, sem `CreateTicket` |
| token transitório antes do ticket | `integration_unavailable` | 503 | `retryable_error` |
| token/config permanentemente rejeitado | `integration_auth_failed` | 502 | `failed`, sem request de ticket |
| `CreateTicket` retorna `400/403/409/404/422` válido | `ticket_rejected` | 502 | `failed` |
| `CreateTicket` retorna `401` válido e corrigível | `integration_auth_failed` | 502 | `retryable_error` |
| `CreateTicket` retorna `429` do GLPI | `rate_limited` | 429 | `retryable_error` |
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
- Horários sempre manipulados e persistidos em UTC.
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
- Conteúdo GLPI escapa HTML individualmente por campo do usuário; conteúdo local/API
  permanece texto simples puro. Proibição de `dangerouslySetInnerHTML` no frontend.
- Nenhum erro externo bruto, URL, body, segredo ou header entra em API/log/store.
- Rate limiting in-memory por tenant fica deferido para hardening futuro; single-instance
  e autenticação mitigam riscos no MVP.
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
cmd/server/support_types.go
cmd/server/supportstore.go
cmd/server/supportstore_test.go
cmd/server/support_integration.go
cmd/server/support_integration_test.go
cmd/server/supportapi.go
cmd/server/supportapi_test.go
```

Alterar aditivamente:

```text
cmd/server/server.go       # campos, store e clientes; sem worker; recuperação de órfãos no boot
cmd/server/httpapi.go      # registerSupportRoutes + Idempotency-Key no CORS
cmd/server/settingsapi.go  # somente features.support bool
internal/glpi/client.go    # adição de GetTicket para reconciliação segura
internal/glpi/types.go     # adição do DTO Ticket
```

Não alterar `internal/tactical` nem outros módulos de `internal/glpi` além das adições acima
destinadas exclusivamente à reconciliação segura.

## 15. Sequência de implementação futura

0. **Pré-requisito concluído:** `T-B002-HARNESS-MARIADB-STORES.md` implementado e validado (`commit e4d3966`).
1. Revisão final de especificação documental concluída. Aguardar autorização formal antes de iniciar código.
2. Implementar DDL específico SQLite/MariaDB e executar suíte contratual de store nos dois bancos
   (`support_types.go`, `supportstore.go`, `supportstore_test.go`).
3. Implementar store: criação+claim atômicos em `processing` (eliminando requests presas em `new`),
   replay de idempotência, enriquecimento condicionado ao token, CAS de retry e finalização, e recuperação
   de órfãos no boot.
4. Cobrir concorrência real entre processos/conexões independentes e atomicidade estrita com auditoria.
5. Adicionar `GetTicket` em `internal/glpi/client.go` e struct `Ticket` em `internal/glpi/types.go` com validação de `external_id`.
6. Implementar orquestrador (`support_integration.go`) com mocks; cobrir sanitização HTML individual/anti-dupla codificação,
   classificação de erros GLPI e enriquecimento de snapshot.
7. Implementar handlers (`supportapi.go`), autorização por conversa, endpoint de retry com disputa 409,
   reconciliação administrativa com validação remota de `external_id`, recovery admin e PUT /device com `glpiContextUpdated: false`.
8. Fazer wiring no boot (`server.go`), CORS (`httpapi.go`) e expor somente `features.support` (`settingsapi.go`).
9. Executar validação completa SQLite/MariaDB, build e suíte offline; registrar evidências nos documentos de controle.

Sem runner de retry em qualquer passo.

## 16. Plano de testes obrigatório

Todos offline quanto a GLPI/Tactical/WhatsApp, determinísticos e sem credenciais
reais. MariaDB usa instância descartável exclusiva de teste.

#### Contrato de store — executar igualmente em SQLite e MariaDB

- DDL sobe duas vezes, preserva dados, tipos, nullability, constraints e índices;
- CRUD e todas as transições permitidas/proibidas;
- criação de request e eventos iniciais (`created` + `ticket_claimed`) é atômica na mesma transação;
- teste provando que não existe request permanentemente presa em `new`;
- teste simulando queda/crash entre a persistência inicial e a chamada externa, comprovando que
  `RecoverOrphanedProcessing` recupera a linha para `unknown` após o cutoff;
- enriquecimento de snapshot na Transação 2 condicionado ao `processing_token`, abortando se o claim
  tiver sido perdido;
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
- resposta HTTP 429 do GLPI termina `retryable_error` e preserva `Retry-After`;
- transporte/timeout/cancelamento/5xx/sucesso inválido termina `unknown`;
- replay e retry explícito em `unknown` nunca executam segundo POST;
- `external_id` tem formato exato, determinístico e não sensível;
- fingerprint v2 cobre presença/normalização de todos os campos semânticos;
- sanitização e proteção XSS: teste com tags HTML (`<script>`, `<div>`), `&`, aspas e quebras de linha
  (`\n` para `<br>`), prevenindo injeção e evitando dupla codificação;
- cliente GLPI `GetTicket` busca ticket por ID e retorna `Ticket{ID, ExternalID}`;
- device binding encontrado, ausente, conflitante e de outro tenant;
- Tactical encontrado, offline, indisponível e ambíguo;
- GLPI encontrado, ausente, indisponível e ambíguo;
- hostname exato, HTML escapado e snapshot textual conforme D-015;
- nenhuma chamada externa real.

### HTTP (`httptest.NewRequest` + `httptest.NewRecorder`)

- rotas novas sem alterar chat;
- request, device e conversa de outro tenant retornam `404`;
- usuário do mesmo tenant sem acesso à conversa recebe `403`;
- atendente comum cria ticket e pode retry de `retryable_error` na conversa autorizada;
- atendente comum vincula/troca binding do mesmo tenant;
- tenant/owner/actor/permissões enviados no body são rejeitados;
- usuário não-admin não reconcilia nem força recovery de `processing`;
- disputa concorrente no `/retry`: vencedor do CAS executa e responde `202`, perdedor responde `409 state_conflict`;
- `POST /retry` em qualquer estado diferente de `retryable_error` (`processing`, `unknown`, `synced`, `failed`)
  responde `409 state_conflict` sem chamar GLPI;
- reconciliação segura (`POST /reconcile` com `outcome=synced`):
  - ticket GLPI existente e com `external_id` correspondente -> transiciona `unknown -> synced` e deriva ID/href oficiais da resposta do GLPI;
  - teste de href arbitrário/malicioso no payload -> comprova que href do usuário é ignorado e derivado com segurança;
  - ticket GLPI com `external_id` divergente -> responde `422 validation_failed` (`reconcile_external_id_mismatch`) e mantém `unknown`;
  - ticket GLPI inexistente -> responde `404 ticket_not_found` e mantém `unknown`;
- recuperação administrativa `outcome=processing_orphaned`:
  - solicitação em `processing` com `processing_started_at <= cutoff` -> CAS para `unknown` e responde `200`;
  - solicitação em `processing` anterior ao cutoff (`processing_started_at > cutoff`) ou com token alterado -> responde `409 state_conflict`;
  - rejeita qualquer ticket ID/href enviado;
- `PUT /device` durante `processing`: responde `200 OK` com `glpiContextUpdated: false`, altera apenas seleção local e não interfere no snapshot congelado;
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
gofmt -l cmd/server/support*.go cmd/server/server.go cmd/server/httpapi.go cmd/server/settingsapi.go internal/glpi/*.go
go vet ./cmd/server/... ./internal/glpi/...
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

1. DDL e suíte contratual passam 100% em SQLite e MariaDB usando o harness descartável da T-B002.
2. Tipos, tamanhos, nullability, índices e uniques seguem a seção 4; `SQLDialect` explícito (`DialectSQLite` em produção; proibição de importar `internal/testdb` em código de produção).
3. Criação atômica (create+claim na mesma transação com `created` e `ticket_claimed`), provando em teste que não existe request permanentemente presa em `new`.
4. Enriquecimento de snapshot na Transação 2 condicionado estritamente ao `processing_token`, abortando se o claim tiver sido perdido.
5. Replay de criação segue a tabela 7.1; requisições concorrentes com mesma chave/fingerprint são serializadas e respondem `202`, nunca `state_conflict`.
6. Mesma chave com fingerprint divergente retorna `409 idempotency_key_reused`.
7. Disputa no `/retry` resolvida por CAS: vencedor executa e responde `202`; perdedor recebe `409 state_conflict` sem efetuar segundo POST.
8. Queda simulada de processo entre persistência e chamada externa é coberta por `RecoverOrphanedProcessing` no boot e recovery administrativo, respeitando cutoff (`orphanAge`).
9. Falha de validação/autenticação pré-ticket classifica `failed|retryable_error`; ambiguidade após chamada `CreateTicket` transita para `unknown`.
10. `unknown` nunca repete automaticamente e só transiciona por reconciliação administrativa autorizada (`IsAdmin()`).
11. Reconciliação com `outcome=synced` invoca `GetTicket` no GLPI (confirmado pelo OpenAPI v2.3 em `/Assistance/Ticket/{id}` com schema `Ticket`), confirma existência e igualdade estrita de `external_id`; divergência retorna `422 reconcile_external_id_mismatch`.
12. Reconciliação rejeita/ignora href arbitrário enviado no payload e deriva ID e href oficiais estritamente da resposta do GLPI (coberto com teste de href malicioso).
13. Recuperação administrativa `outcome=processing_orphaned` exige `IsAdmin()`, rejeita ticket ID e só recupera se `processing_started_at <= cutoff`, retornando `409` em caso de concorrência ou cutoff não atingido.
14. Atendente comum cria, consulta e vincula/troca equipamento apenas em conversa autorizada; IDOR e tentativas de injetar tenant/owner/permissões via body são bloqueadas.
15. `PUT /device` durante `processing` altera apenas seleção local, responde `200` com `glpiContextUpdated: false`, não altera snapshot congelado e não concorre com enriquecimento.
16. Conteúdo GLPI (`TicketInput.Content`) passa por escape individual com `html.EscapeString` para cada campo do usuário antes de converter `\n` para `<br>`; coberto com testes contra tags HTML, &, aspas e dupla codificação; proibição de `dangerouslySetInnerHTML`.
17. Tactical permanece estritamente somente leitura; indisponibilidade não quebra a criação do ticket.
18. Rate limiting in-memory por tenant fica explicitamente fora da T-005 (transferido para hardening futuro); cliente GLPI continua tratando 429 externo com `retryable_error` e preservando `Retry-After`.
19. Rotas/modelo de chat, Flow Builder, portal e SSE permanecem rigorosamente inalterados.
20. Build, vet e suítes completas rodam offline sem credenciais reais ou chamadas de rede externas.

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

### Status dos bloqueios

- **Harness MariaDB (T-B002):** CONCLUÍDO e aceito (`commit e4d3966`). O harness
  descartável (`test/mariadb/compose.yml`, `internal/testdb` e scripts de teste) está
  disponível e validado para apoiar os testes contratuais da T-005.
- **Implementação T-005:** BLOQUEADA aguardando autorização explícita. Não implementar
  código runtime nem realizar push até aprovação formal.

### Dúvidas não bloqueantes para o backend mínimo

1. Existe busca GLPI operacional confiável por `external_id`?
   - O OpenAPI oficial GLPI v2.3 (`GET /Assistance/Ticket/{id}`) confirmou que o DTO
     `Ticket` retorna `external_id`, viabilizando a validação pontual de integridade no
     reconcile. Uma busca indexada reversa não é necessária no MVP.
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

Revisão documental concluída e finalizada em 2026-09-12 com todas as decisões aprovadas
incorporadas (single-instance, ausência de rate limiter in-memory na T-005, disputa 409
no retry, escape seguro de HTML no GLPI, create+claim atômicos eliminando request presa em
new, reconciliação segura via GetTicket validando external_id contra spoofing, recuperação
administrativa condicionada a cutoff e isolamento de dialeto).
T-B002 aprovada e concluída. Implementação da T-005 pronta para ser iniciada assim que
houver autorização explícita. Nenhum código de produção ou teste de runtime foi alterado
nesta etapa. Push bloqueado até autorização.
