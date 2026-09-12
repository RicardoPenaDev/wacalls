# T-004 — Clientes `internal/glpi` + `internal/tactical`

Status: **concluída** (implementação e testes offline finalizados em 2026-09-12).
Escopo desenhado em `docs/tasks/T-002-*.md` (§Serviços de integração), D-014 e D-015.

> **Nada de segredo** em código, teste, fixture, log, snapshot, STATUS ou commit.
> Testes 100% offline (`httptest`); **nenhuma** chamada de rede real; **nenhuma**
> ação real em GLPI/Tactical (AGENTS §4, SECURITY §Operação segura).

## Resultado esperado

Dois clientes HTTP pequenos e testáveis, atrás de interfaces mínimas consumíveis
por mock: `internal/glpi` (High-Level REST API v2.3, OAuth2, criação de ticket e
resolução de computador por hostname) e `internal/tactical` (**somente leitura**
de agente/cliente/site e status). Cada cliente tem `http.Client` com timeout,
erros tipados, redaction e testes `httptest` cobrindo sucesso e falhas. Sem
rotas, stores ou wiring no `cmd/server` (isso é T-005).

## Inventário confirmado no código (read-only, 2026-09-12)

- **Não existe** `internal/glpi` nem `internal/tactical` hoje (nenhum código).
- Padrão HTTP: `&http.Client{Timeout: N*time.Second}` + `http.NewRequestWithContext`
  (`billingapi.go`, `transcriber.go`, `flowbridge.go`, `license.go`).
- Config por env: `strings.TrimSpace(os.Getenv("WACALLS_*"))`; parsing de flag
  boolean por conjunto de valores (`license.go`: `1|on|true|sim|yes`). Não há
  loader central — cada subsistema lê o env que precisa.
- Feature flag exposta ao client via `features` em `GET /api/settings/options`
  (`settingsapi.go`); master do suporte = `WACALLS_SUPPORT_ENABLED` (T-002).
- **Não há** helper de redaction hoje — T-004 adiciona o mínimo (nunca logar
  credenciais OAuth2, Bearer token ou `X-API-KEY`; redigir query/userinfo da URL).

## A. Escopo GLPI (mínimo) — `internal/glpi`

Fonte confirmada: OpenAPI 3.0 da **GLPI High-Level REST API 2.3.0** da instância
GLPI 11.0.8, `doc.json` com 4.648.016 bytes e SHA-256
`8c6a63e64caf740414863de1a1257129f2c61dc76d9c1b747cccd43fd821f9e6`.
O arquivo completo **não é versionado**.

API legada `apirest.php` permanece desligada. A integração usa somente OAuth2 e
rotas sob `/api.php/v2.3`. O token é obtido em `POST /api.php/token`; chamadas da
API usam Bearer token. `client_credentials` continua restrito ao inventário e
não atende a criação de tickets.

Homologação validou um cliente OAuth exclusivo com password grant e scope
`api`, conta técnica dedicada e Bearer token com `expires_in` próximo de 3600s.
Credenciais e tokens permanecem fora do repositório; perfil/entidades mínimos e
restrição de IP continuam requisitos operacionais.

| Operação | HTTP v2.3 | Escopo |
|---|---|---|
| obter token | `POST /api.php/token` (OAuth2 password grant) | T-004 |
| localizar computador | `GET /api.php/v2.3/Assets/Computer` com `filter` RSQL por `name` e resultado limitado | T-004 |
| obter computador | `GET /api.php/v2.3/Assets/Computer/{id}` | T-004 |
| criar ticket | `POST /api.php/v2.3/Assistance/Ticket` | T-004 |
| obter ticket | `GET /api.php/v2.3/Assistance/Ticket/{id}` | T-005+ |
| atualizar ticket | `PATCH /api.php/v2.3/Assistance/Ticket/{id}` | T-005+ |
| listar followups | `GET /api.php/v2.3/Assistance/Ticket/{id}/Timeline/Followup` | T-005+ |
| criar followup | `POST /api.php/v2.3/Assistance/Ticket/{id}/Timeline/Followup` | T-005+ |

Regras GLPI:

- `FindComputerByHostname` envia `filter` RSQL de igualdade sobre o campo
  `name`, limita a coleção e reaplica igualdade exata com `normalizeHostname`
  (T-003). Zero correspondências exatas → `ErrNotFound`; mais de uma →
  `ErrAmbiguous`. Nunca usar `search/Computer` da API legada nem fuzzy match.
- `CreateTicket` envia **diretamente** o schema `Ticket`, sem envelope
  `{"input": ...}`. Campos relevantes: `name`, `content`, `type`, `urgency`,
  `impact`, `priority`, `entity`, `location`, `category`, `request_type`,
  `user_recipient` e `external_id`. A resposta `201` fornece `id` e `href`.
- `external_id` pode carregar a referência do WACalls, mas a idempotência
  definitiva permanece em `support_requests.idempotency_key` (T-005).
  `CreateTicket` nunca tem retry automático sem verificação idempotente.
- Headers contextuais disponíveis: `GLPI-Entity`, `GLPI-Profile`,
  `GLPI-Entity-Recursive` e `Accept-Language`. Somente configuração confiável
  pode defini-los; dados do usuário não podem sobrescrevê-los.
- O OpenAPI contém o schema `Ticket_Item`, mas **não publica rota v2.3** para
  criar a associação Ticket↔Computer. `POST Item_Ticket` pertence à API legada
  e está proibido. `LinkComputerToTicket` fica fora da interface e da
  implementação inicial. Provisoriamente, `content` pode registrar hostname e
  GLPI Computer ID; isso é contexto textual, **não vínculo nativo**.

## B. Escopo Tactical (mínimo, SOMENTE LEITURA) — `internal/tactical`

Fonte: docs.tacticalrmm.com/functions/api + **dois contratos confirmados em
homologação (respostas sanitizadas, 2026-09-12)**. Auth por header `X-API-KEY`
(**nunca** o Authorization de sessão do navegador); base URL só via
`WACALLS_TACTICAL_BASE_URL`; **barras finais obrigatórias** (Django).

| Operação | HTTP | Confirmação |
|---|---|---|
| auth | header `X-API-KEY: <key>` em toda requisição | Confirmado (homolog) |
| listar agentes | `GET /agents/` → `200`, `application/json`, **array JSON direto** | Confirmado (homolog) |
| obter agente por id | `GET /agents/{agent_id}/` → `200`, `application/json`, **objeto JSON direto** (resposta grande, centenas de KB) | Confirmado (homolog) |
| localizar por hostname | `GET /agents/` + match `hostname` normalizado exato | Confirmado; filtro server-side opcional **a confirmar** (não bloqueia MVP) |

**DTOs HTTP privados, um por endpoint**, convertidos por mapper para o DTO
interno estável `tactical.Agent` (§C). Os endpoints divergem em nomes de campo:

| DTO interno | `GET /agents/` (lista) | `GET /agents/{id}/` (detalhe) |
|---|---|---|
| ClientName | `client_name` | `client` (⚠ **não** assumir `client_name`) |
| SiteName | `site_name` | `site_name` |
| SiteID | — | `site` (ID numérico) |
| LoggedUser | `logged_username` | `logged_in_username` |
| LastLoggedUser | — | `last_logged_in_user` |
| Version / Plat | — | `version` / `plat` |

Ambos mapeiam ainda: `agent_id`, `hostname`, `status`, `last_seen` (RFC3339),
`monitoring_type`, `operating_system`, `local_ips`, `serial_number` (se presente),
`needs_reboot` (bool), `maintenance_mode` (bool). Os aliases de usuário são
tratados **explicitamente no mapper** (lista `logged_username`; detalhe
`logged_in_username`).

Decodificação e segurança (confirmado):
- ignorar campos JSON desconhecidos; ausentes/opcionais não quebram o decode;
- **não** decodificar/persistir/logar: `custom_fields`, `public_ip`,
  `mesh_node_id`, `services`, WMI, políticas, `all_timezones`, hardware detalhado,
  nem a resposta bruta;
- **limite de body = 2 MiB** (constante `maxAgentBodyBytes`, testada); acima →
  `ErrBadResponse` **sem** registrar o conteúdo;
- erro/log nunca incluem o response body completo.

Pertencem à **T-004**: `Ping/CheckConfig`, `ListAgents`, `GetAgent`
(`GET /agents/{id}/`), `FindAgentByHostname` (lista + match exato normalizado;
múltiplo exato → `ErrAmbiguous`; **sem** fuzzy). Ficam para **T-005+**: cache TTL
(T-007), busca por unidade, badge "desatualizado". **Proibido** em qualquer fase:
script, terminal, reboot, acesso remoto (SECURITY §Permissões; D-014).

## C. Contratos internos (interfaces pequenas + DTOs)

DTOs internos, **independentes do JSON bruto** das APIs (só campos usados):

```text
glpi.Computer{ ID, Name, SerialNumber?, Entity? }
glpi.TicketInput{ Name, Content, Type?, Urgency?, Impact?, Priority?,
                  Entity?, Location?, Category?, RequestType?, UserRecipient?,
                  ExternalID? }
glpi.CreatedTicket{ ID, Href }
tactical.Agent{ AgentID, Hostname, ClientName, SiteName, SiteID, Status,
                LastSeen(time), MonitoringType, OperatingSystem, LoggedUser,
                LastLoggedUser, LocalIPs, SerialNumber, NeedsReboot(bool),
                MaintenanceMode(bool), Version, Plat }
```

Interfaces mínimas (declaradas no **consumidor** `cmd/server` na T-005 — idioma
Go; a T-004 entrega os structs concretos que as satisfazem):

```text
glpiClient interface {
    FindComputerByHostname(ctx, hostname string) (glpi.Computer, error)  // ErrNotFound/ErrAmbiguous
    GetComputer(ctx, id string) (glpi.Computer, error)
    CreateTicket(ctx, in glpi.TicketInput) (glpi.CreatedTicket, error)
}
tacticalClient interface {                 // somente leitura
    FindAgentByHostname(ctx, hostname string) (tactical.Agent, error)    // ErrNotFound/ErrAmbiguous
    GetAgent(ctx, agentID string) (tactical.Agent, error)
}
```

`LinkComputerToTicket` não integra a interface inicial: não há rota v2.3
publicada e uma operação que sempre retorna “não suportada” criaria capacidade
enganosa. Construtores: `glpi.New(cfg glpi.Config) (*glpi.Client, error)` e
`tactical.New(cfg tactical.Config) (*tactical.Client, error)`; falham em config
incompleta/inválida. Nada de variáveis globais.

A normalização privada destes clientes replica deliberadamente apenas
`strings.TrimSpace` + `strings.ToUpper`, sem alterar/importar o código T-003 de
`cmd/server`. Essa duplicação é controlada: extrair um helper compartilhado
ampliaria o escopo. A T-005 deve revalidar o hostname na fronteira de domínio
antes de persistir ou vincular resultados externos.

## D. Configuração (somente nomes — nunca valores/tokens)

- GLPI/OAuth2: `WACALLS_GLPI_BASE_URL`, `WACALLS_GLPI_CLIENT_ID`,
  `WACALLS_GLPI_CLIENT_SECRET`, `WACALLS_GLPI_USERNAME`,
  `WACALLS_GLPI_PASSWORD`, `WACALLS_GLPI_ENTITY_ID`,
  `WACALLS_GLPI_PROFILE_ID`, `WACALLS_GLPI_ENTITY_RECURSIVE`,
  `WACALLS_GLPI_ACCEPT_LANGUAGE` e `WACALLS_GLPI_TIMEOUT_SECONDS`.
  Scope fixo: `api`; token endpoint fixo: `/api.php/token`; API base fixa:
  `/api.php/v2.3`.
- Tactical: `WACALLS_TACTICAL_BASE_URL`, `WACALLS_TACTICAL_API_KEY`,
  `WACALLS_TACTICAL_TIMEOUT_SECONDS` (opcional).
- Master/flag: `WACALLS_SUPPORT_ENABLED` (T-002). Desligado ⇒ clientes não são
  construídos e o painel/rotas ficam ocultos (T-005/T-006).

> **Substitui a configuração GLPI preliminar da T-002:** não usar
> `WACALLS_GLPI_TOKEN`, `APP_TOKEN`, `USER_TOKEN` ou `Session-Token`; pertencem
> ao contrato legado desativado.

Config falha de forma clara quando a integração está **habilitada e incompleta**:
`New(...)` retorna `ErrConfig` nomeando a variável ausente/inválida, sem revelar
valores. `.env.example` recebe apenas nomes e placeholders não secretos.

## E. HTTP e segurança

- `&http.Client{Timeout}` por cliente (default sugerido 15–20s GLPI, 10–15s
  Tactical); `http.NewRequestWithContext` sempre; `defer resp.Body.Close()`.
- Corpo lido por `io.LimitReader` (teto ~2 MiB); acima ⇒ `ErrBadResponse`.
- Base URL só do env (operador confiável). Validar esquema `https?://` e host
  não vazio; **nunca** aceitar host/URL vindo de dado de usuário (anti-SSRF).
  Sem seguir redirect para outro host.
- Mapa de status → erro tipado (ver matriz). `Retry-After` (429/503) é **lido** e
  devolvido em `ErrRateLimited{RetryAfter}` para o runner (T-007) respeitar; os
  clientes não repetem operações mutáveis automaticamente.
- O cliente GLPI adquire e mantém o Bearer token apenas em memória, renovando-o
  antes da expiração. Uma leitura rejeitada por token expirado pode renovar e
  repetir uma vez; `CreateTicket` **nunca** é repetido automaticamente.
- **Redaction:** nunca logar `Authorization`, `X-API-KEY`, Client Secret,
  username, password, access/refresh token nem query string; erros nunca embutem
  segredo.
- TLS verify permanece ligado; não há flag nem opção de configuração para
  `InsecureSkipVerify`.

## F. Testes obrigatórios (offline, `httptest`)

Um `httptest.Server` por caso; sem rede. Cobrir:

1. OAuth2 password grant usa `POST /api.php/token`, scope `api`, e as chamadas
   v2.3 enviam Bearer token sem expor credenciais;
2. sucesso e parse dos DTOs GLPI/Tactical;
3. não encontrado → `ErrNotFound`;
4. não autorizado (`401/403`) → `ErrAuth`;
5. rate limit (`429` + `Retry-After`) → `ErrRateLimited` com duração;
6. erro `5xx` → `ErrUnavailable`;
7. timeout/cancelamento → erro de contexto;
8. JSON inválido ou corpo excessivo → `ErrBadResponse`;
9. segredo não aparece em `err.Error()`;
10. hostname exato normalizado casa; parcial/fuzzy não; múltiplo exato →
    `ErrAmbiguous`;
11. busca de Computer envia `filter` RSQL por `name`, limita resultados e não
    acessa rota legada;
12. `CreateTicket` envia schema `Ticket` direto, sem `input`, lê `201` com
    `id`/`href` e realiza exatamente um POST mesmo em falha;
13. headers contextuais GLPI configurados são enviados;
14. Tactical respeita DTOs distintos de lista/detalhe e limite de 2 MiB;
15. mocks das interfaces compilam em teste de exemplo.

## G. Entregas futuras (separação explícita)

- **T-004 (esta):** clientes HTTP `internal/glpi` + `internal/tactical` e
  contratos, testados offline; sem vínculo nativo Ticket↔Computer.
- **T-005:** `support_requests` store + `supportapi.go` + wiring no `cmd/server`;
  criação idempotente de ticket, `sync_state` e contexto textual do computador.
- **T-006:** `SupportPanel` no client + `features["support"]`.
- **T-007:** runner de retry + cache TTL do Tactical + auditoria. Operações GLPI
  futuras de ticket/followup só entram com idempotência definida.

## Arquivos previstos (criar na implementação)

```text
internal/glpi/client.go        internal/glpi/types.go        internal/glpi/errors.go
internal/glpi/client_test.go
internal/tactical/client.go    internal/tactical/types.go    internal/tactical/errors.go
internal/tactical/client_test.go
.env.example                   (adicionar os nomes WACALLS_GLPI_* / WACALLS_TACTICAL_*)
```

## Matriz de erros (tipados; compartilham a mesma forma nos dois pacotes)

| Situação | HTTP | Erro |
|---|---|---|
| indisponível / 5xx / conexão | 500–599, dial fail | `ErrUnavailable` |
| não autenticado/autorizado | 401, 403 | `ErrAuth` |
| não encontrado | 404 (ou busca vazia) | `ErrNotFound` |
| conflito | 409 | `ErrConflict` |
| rate limit | 429 (+`Retry-After`) | `ErrRateLimited` |
| múltiplo match exato | — | `ErrAmbiguous` |
| JSON inválido / corpo excessivo | 2xx corrompido | `ErrBadResponse` |
| config habilitada e incompleta | — | `ErrConfig` |

## Não alterar nesta tarefa

- `cmd/server/*` (rotas, `server.go`, `httpapi.go`, `settingsapi.go`) — é T-005.
- Flow Builder (`flowexec*.go`, `flowbridge.go`, `flow{api,store}.go`),
  `messageapi.go`, o modelo `(session_id, chat_jid)`, arquivos VoIP.
- `cmd/server/devicebindingstore.go`/`hostname.go` (T-003, já pronto).
- Não criar `conversation_id`. Não refatoração ampla.

## Decisões confirmadas e pendências antes do piloto

- **Q1 (GLPI/API) — RESOLVIDA:** GLPI 11.0.8 usa exclusivamente High-Level REST
  API 2.3.0 sob `/api.php/v2.3`, OAuth2 e `POST /api.php/token`. API legada
  desligada.
- **Q2 (busca Computer) — RESOLVIDA:** coleção `Assets/Computer`, `filter` RSQL
  por `name`, resultado limitado e igualdade novamente validada por
  `normalizeHostname`.
- **Q3 (idempotência):** `support_requests.idempotency_key` continua como
  garantia definitiva na T-005; `external_id` leva a referência do WACalls.
  T-004 não repete `CreateTicket`.
- **Q4 (Tactical) — RESOLVIDA para o MVP:** `GET /agents/` e
  `GET /agents/{id}/`, barras finais, auth `X-API-KEY`, DTOs privados por
  endpoint e limite de 2 MiB. Filtro server-side por hostname segue opcional.
- **Q5 (TLS/rede) — RESOLVIDA:** produção e homologação exigem TLS válido;
  T-004 não expõe opção `*_INSECURE_TLS`.
- **Q6 (OAuth operacional) — RESOLVIDA EM HOMOLOGAÇÃO:** cliente OAuth
  exclusivo aceita password grant, scope `api` e Bearer com expiração próxima
  de 3600s. Credenciais não foram registradas; privilégios/entidades mínimos e
  restrição de IP permanecem requisitos de implantação.
- **Q7 (Ticket↔Computer) — BLOQUEIO CONFIRMADO:** não há rota v2.3 publicada.
  Não usar `Item_Ticket`, API legada ou banco. O vínculo nativo fica deferido;
  T-005 registra hostname e Computer ID no conteúdo como limitação explícita.

## Critérios de aceite da implementação (T-004)

1. `internal/glpi` e `internal/tactical` compilam; `go build ./...` ok.
2. Testes offline cobrem a matriz F e os contratos HTTP confirmados.
3. GLPI usa OAuth2 password grant e apenas `/api.php/token` + `/api.php/v2.3`;
   nenhuma rota/header da API legada.
4. Busca de Computer usa RSQL por `name`, limite e igualdade normalizada.
5. `CreateTicket` usa body direto, retorna `id`/`href` e não tem auto-retry.
6. `LinkComputerToTicket` não existe na interface inicial.
7. Erros tipados; segredo nunca aparece em erro/log.
8. `New(...)` falha claramente com configuração habilitada incompleta.
9. Tactical permanece somente leitura.
10. Nenhum segredo em código/teste/fixture/log.

## Validação executada

```text
gofmt -l internal/glpi internal/tactical                         # sem saída
go vet ./internal/glpi/... ./internal/tactical/...               # OK
go test ./internal/glpi/...                                      # OK
go test ./internal/tactical/...                                  # OK
go build ./...                                                   # OK
go test ./...                                                    # OK
git diff --check                                                 # OK
```

## Resultado da implementação

- Criados `internal/glpi/{client,types,errors}.go` e testes: OAuth2 password
  grant, cache concorrente com margem de expiração, uma renovação em GET após
  `401`, busca exata de Computer, leitura por ID e criação não repetida de
  Ticket com schema direto.
- Criados `internal/tactical/{client,types,errors}.go` e testes: `X-API-KEY`,
  lista/detalhe com DTOs privados distintos, mapper para `Agent`, busca exata e
  contrato estritamente read-only.
- Ambos limitam respostas a 2 MiB, fecham bodies, bloqueiam redirect para outra
  origem, preservam TLS seguro e retornam erros tipados sem URL, segredo ou body.
- `.env.example` contém somente nomes de configuração e valores vazios.
- Nenhum código T-003, rota, store, wiring, T-005 ou API externa real foi
  alterado/executado. `LinkComputerToTicket` permanece deferido pela ausência de
  rota v2.3; T-005 deve revalidar hostname e registrar Computer ID como contexto
  textual.
