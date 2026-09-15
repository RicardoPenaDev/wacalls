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

## D-015 — GLPI somente pela High-Level REST API v2.3

Data: 2026-09-12 · Status: aceita

Decisão: WACalls integra com GLPI 11 exclusivamente pela High-Level REST API
v2.3 e OAuth2. O cliente operacional será exclusivo do WACalls, com password
grant, scope `api`, conta técnica e menor privilégio. A API legada permanece
desligada e o WACalls nunca acessa diretamente o banco do GLPI.
Motivo: o OpenAPI 3.0 real da API 2.3.0 confirma Computer e Ticket, mas não
publica rota para criar vínculo Ticket↔Computer.
Alternativas consideradas: habilitar `apirest.php` para usar `Item_Ticket`,
gravar diretamente no banco ou expor `LinkComputerToTicket` sempre não
suportado — rejeitadas por segurança, acoplamento e contrato enganoso.
Consequência: T-004 não implementa `LinkComputerToTicket`. Até surgir rota v2
oficial, hostname e GLPI Computer ID podem constar no conteúdo do ticket como
contexto textual, sem equivaler a vínculo nativo. O piloto depende de provisionar
o novo cliente OAuth; o cliente atual “API Teste” não possui password grant.

## D-016 — Resultado ambíguo de criação GLPI exige reconciliação

Data: 2026-09-12 · Status: aceita

Decisão: a T-005 persiste a solicitação antes do `CreateTicket` e usa claim
atômico/token no banco. “Tentativa iniciada” significa que o consumidor chamou
`CreateTicket`; não significa que a aplicação sabe se o GLPI recebeu o body.
Erro de transporte, timeout, cancelamento, `5xx` ou resposta inválida dessa
chamada deixa `support_requests.sync_state='unknown'`. Resposta HTTP válida que
prove rejeição e falha tipada anterior ao request de ticket não são ambíguas.
`unknown` não tem retry automático e só sai por reconciliação autorizada.
Motivo: o cliente GLPI executa operação não repetível e a API disponível não
oferece hoje reconciliação por `external_id`; repetir pode duplicar chamado.
Alternativas consideradas: tratar toda falha como repetível, inferir entrega do
body pela camada HTTP ou usar somente mutex em memória — rejeitadas por
duplicação, informação indisponível, concorrência entre processos e reinício.
Consequência: finalização exige o mesmo token do claim. Recuperação explícita
converte somente `processing` comprovadamente expirado para `unknown`, por CAS,
com evento na mesma transação; leitura nunca converte estado. O worker de D-014
fica deferido até existir reconciliação remota confiável.

## D-017 — T-005 exige instância única no MVP

Data: 2026-09-12 · Status: aceita

Decisão: enquanto a T-005 estiver no MVP, cada implantação terá uma única
instância ativa do servidor. A recuperação no startup converte para `unknown`
somente claims `processing` anteriores ao cutoff configurado; leitura nunca
recupera estado. MariaDB da T-B002 é infraestrutura descartável de testes, não
backend habilitado para escalar o servidor.
Motivo: a implantação atual declara um container WACalls, usa SQLite local com
pool de uma conexão e mantém broker, sessões e rate limits em memória.
`internal/storage` rejeita MariaDB para o servidor, e não há leader election,
lease ou coordenação distribuída.
Alternativas consideradas: lease renovável, recovery somente administrativa ou
heartbeat distribuído. Lease/heartbeat adicionariam coordenação inexistente; recovery só
manual deixaria claims órfãos sem tratamento automatizado após crash.
Consequência: startup recovery é seguro sob a restrição single-instance e ainda
respeita deadline total + margem. Suporte multi-instância futuro deve substituir
essa decisão por lease/coordenação antes de habilitar réplicas.

## D-018 — Create+claim atômico e reconciliação segura com validação remota de external_id

Data: 2026-09-12 · Status: aceita

Decisão: na T-005, a criação de `support_requests` persiste a linha e reivindica o claim
na mesma transação inicial (estado `processing`, com `processing_token`,
`processing_started_at` e eventos `created` + `ticket_claimed`). Não há estado `new`
persistido sem claim ativo, eliminando requests órfãs em caso de crash precoce.
Adicionalmente, `POST /api/support/requests/{id}/reconcile` com `outcome=synced` exige
verificação remota via `GetTicket` no GLPI, confirmando a existência do ticket e a
igualdade estrita de `external_id`, derivando ID e href da resposta oficial do GLPI e
rejeitando href arbitrário fornecido pelo cliente. O rate limit local fica deferido
para hardening posterior.
Motivo: evitar que falhas entre a inserção e o claim deixem registros inacessíveis a retry
e recovery, e impedir que reconciliação administrativa vincule chamados incorretos ou
injete links maliciosos sem validação remota.
Alternativas consideradas: manter `new` com recovery dedicado de `new` órfão, aceitar
href arbitrário no reconcile ou confiar cegamente no ID GLPI fornecido pelo admin.
Consequência: o ciclo de vida inicial é imune a interrupções não recuperáveis; GLPI
ganha método de consulta `GetTicket`; e o rate limit de 10 claims/60s não entra no escopo
inicial do MVP.

## D-019 — Avaliação de arquitetura de rate limiting para claims

Data: 2026-09-13 · Status: proposta

Decisão proposta: Avaliar na Etapa 7.5 entre reverse proxy (Nginx/Caddy), middleware persistente em Redis/banco ou manutenção da estratégia atual de idempotência atômica e CAS no banco de dados.
Motivo: Evitar decisões prematuras de infraestrutura pesada no MVP sem evidência de saturação.
Alternativas consideradas: Adicionar Redis imediatamente no MVP — rejeitada.
Consequências: O MVP prossegue com a proteção de concorrência por chave de idempotência e transações atômicas de claim.

## D-020 — Hardening do link da interface web do GLPI via backend seguro (Opção A restrita)

Data: 2026-09-13 · Status: aceita

Decisão: O link web do ticket no painel de suporte é gerado e validado exclusivamente pelo backend a partir da configuração `WACALLS_GLPI_WEB_BASE_URL` e do ID decimal positivo do ticket (`/front/ticket.form.php?id={id}`). O `href` da API v2.3 permanece estritamente interno no domínio/store e é omitido da serialização do DTO público. O DTO público expõe o campo `webUrl`. Caso `WACALLS_GLPI_WEB_BASE_URL` não seja informada, o link web permanece desabilitado (`webUrl` nulo) e a interface exibe apenas a ação "Copiar número". O frontend aplica validação defensiva complementar (exigência de protocolo `https:`, rejeição de credenciais embutidas, esquemas perigosos e URLs malformadas).
Motivo: Prevenir injeção de links maliciosos, impedir vazamento de rotas internas da API REST JSON, e resolver a incompatibilidade entre o endpoint da API v2.3 e a tela gráfica acessível por atendentes humanos.
Alternativas consideradas: Opção B (montagem no frontend com base injetada em tempo de build) e Opção C (remoção definitiva do link mantendo apenas cópia do ID).
Consequências: Acesso seguro e auditado à tela do chamado no GLPI; falha no startup se houver divergência de origem entre a API e a interface web; preservação da cópia de número para chamados legados e ambientes sem link web habilitado.

## D-022 — parseLastSeen do Tactical não assume fuso do formato legado sem offset

Data: 2026-09-15 · Status: aceita

Decisão: `internal/tactical/client.go` (`parseLastSeen`) aceita RFC3339 (com
ou sem offset, com ou sem frações de segundo — que já eram compatíveis com o
parsing original), string vazia/nula e espaços nas bordas sem nunca falhar.
O formato legado relatado inicialmente durante o Gate 4 (`MM/DD/YYYY
HH:mm:ss`, sem offset) é reconhecido — não é confundido com lixo — mas
tratado como não confiável (`LastSeenValid=false`, hora zero) em vez de
assumir UTC ou horário local. Um timestamp inválido ou não confiável nunca
falha `ListAgents`/`GetAgent` nem transforma um agente existente em
"ausente" (`missing_tactical`); apenas a telemetria auxiliar de `last_seen`
fica indisponível, com aviso sanitizado registrado pelo `SupportService`.
Motivo: Uma checagem read-only ao vivo no tenant de homologação em
2026-09-15 encontrou todos os 29 agentes retornando RFC3339 com `Z`,
impossibilitando cruzar o formato legado contra um timestamp real "online"
para confirmar seu fuso. Investigação adicional (mesma data) não encontrou
nenhuma evidência bruta sobrevivente (logs do servidor, scripts de Gate
1/Gate 4) de que a API tenha de fato retornado `last_seen` fora de RFC3339
durante o Gate 4 — `server.stdout.log`/`server.stderr.log` não mencionam
Tactical/last_seen/RicardoSMS nesse período, e o pacote `tactical` não
tinha logger antes desta correção. Uma reprodução controlada mostrou que
converter um `last_seen` RFC3339 real para `[datetime]` no PowerShell e
exibi-lo com `ToString()` padrão nesta máquina produz exatamente uma string
`MM/dd/yyyy HH:mm:ss` em horário local — a mesma forma relatada como
"bruta" no Gate 4 — consistente com artefato de apresentação do PowerShell,
não com conteúdo da API. Não é possível confirmar isso retroativamente com
certeza absoluta; por isso o fuso do formato legado segue tratado como não
confirmado, e não como comprovadamente-UTC nem comprovadamente-local.
Alternativas consideradas: Assumir UTC para o formato legado (rejeitada —
sem evidência); assumir `America/Sao_Paulo` (rejeitada — mesmo motivo);
não tolerar o formato legado (rejeitada — é um defeito real e
independentemente confirmado por inspeção de código: um único registro
malformado em `ListAgents()` derruba `FindAgentByHostname` para qualquer
hostname do tenant, não só o agente afetado; vale como hardening preventivo
mesmo sem certeza de que foi a causa do Gate 4 — a causa comprovada desse
incidente foi `SupportService` descartando `Agent.AgentID` mesmo em lookups
bem-sucedidos, corrigida separadamente).
Consequências: `last_seen` do formato legado fica ausente (zero) até uma
ocorrência real, com log bruto preservado, permitir confirmar o fuso;
identidade do agente (`agent_id`, `hostname`, `status`) nunca é perdida por
causa disso; `device_bindings.tactical_agent_id` passa a ser preenchido
corretamente mesmo quando `last_seen` não é confiável.

## D-023 — Observabilidade sanitizada e categorizada de erros do Tactical (T-007 7.4-R2)

Data: 2026-09-15 · Status: aceita

Decisão: `RefreshDeviceBinding` e a resolução best-effort de equipamento em
`CreateTicket` (`cmd/server/supportservice.go`) só ramificavam em
`tacErr == nil`; qualquer erro de `tactical.FindAgentByHostname` era
descartado sem nenhum log, tornando not-found, falha de autenticação,
rate-limit, timeout e erro 5xx todos indistinguíveis a posteriori. Uma
tentativa real de refresh em 2026-09-15 (binding `RicardoSMS`, pós-7.4-R1)
terminou em `match_status=missing_tactical` sem nenhuma linha de log
explicando por quê — uma checagem manual, read-only, segundos depois,
encontrou o agente presente e válido no Tactical, tornando a causa exata
dessa execução específica não reconstituível. Corrigido em duas camadas:
(1) `internal/tactical` ganha `ErrTimeout` e `ErrCanceled` como `Kind`
tipados — `get()` agora classifica via `errors.Is` sobre o erro retornado
por `http.Client.Do`, o que detecta corretamente tanto o `Timeout`
configurado do próprio cliente quanto o deadline/cancelamento do contexto
do chamador (antes, só `ctx.Err()` era checado, que nunca via o `Timeout`
do cliente disparar sem deadline do chamador — caía no `ErrUnavailable`
genérico, indistinguível de falha de rede/DNS/TLS); (2) `cmd/server` ganha
`tacticalErrorCategory` (mapeia qualquer erro do Tactical para uma
categoria pequena e estável — `not_found`, `auth`, `rate_limited`,
`timeout`, `canceled`, `unavailable`, `parse_error`, `conflict`,
`ambiguous`, `bad_request`, `config`, `unknown` — mais o status HTTP
quando existir, nunca URL, header, corpo de resposta ou API key) e
`hashHostnameForLog` (hash SHA-256 truncado do hostname normalizado —
nunca o hostname em texto plano em nenhum log relacionado a Tactical).
`RefreshDeviceBinding`/`CreateTicket` passam a logar de forma
diferenciada: agente resolvido e válido não loga nada; agente resolvido
com `last_seen` não confiável continua em `WARN` (já existia); agente
encontrado por hostname mas com `agent_id` vazio na resposta upstream
**deixa de ser tratado como `foundTactical=true`** — persistir
`match_status=matched` com `tactical_agent_id` em branco seria pior que
`missing_tactical`, então esse caso agora é logado em `WARN` com uma
mensagem própria ("discarding match") e conta como não resolvido; uma
falha real de consulta loga em `WARN` com categoria e status HTTP;
not-found simples loga em `INFO`, categoria própria, nível deliberadamente
mais baixo que uma falha real.
Motivo: sem essa observabilidade, qualquer falha futura do Tactical durante
um refresh ou criação de chamado repete o mesmo impasse de 2026-09-15 —
resultado observável (`missing_tactical`) sem causa reconstituível — e cada
investigação exigiria instrumentação ad-hoc read-only fora do código
versionado.
Alternativas consideradas: manter o comportamento silencioso e depender
apenas de reprodução ao vivo fora de banda (rejeitada — já provou ser
insuficiente no incidente de 2026-09-15); logar o hostname em texto plano
para facilitar correlação (rejeitada — viola a minimização de dados de
`docs/SECURITY.md`); tratar `agent_id` vazio como `matched` mesmo assim
(rejeitada — persistiria um identificador inútil e uma etiqueta de estado
incorreta no `device_binding`).
Consequências: falhas futuras do Tactical em `refresh_device_binding` e
`create_ticket` são diagnosticáveis diretamente pelo log estruturado, sem
expor segredos ou dados pessoais; nenhuma mudança de comportamento
observável pelo usuário final (o resultado funcional de cada caminho
permanece o mesmo — o que muda é exclusivamente a informação disponível
para diagnóstico); testes cobrem log sanitizado sem hostname cru,
diferenciação not-found vs. falha real, e `agent_id` vazio não virando
`matched` (`cmd/server/supportservice_test.go`).

## D-024 — Parser tolerante para local_ips do Tactical e preservação sanitizada de erro de decode (T-007 7.4-R3)

Data: 2026-09-15 · Status: aceita

Decisão: `internal/tactical/client.go` definia `listAgentDTO.LocalIPs` e
`detailAgentDTO.LocalIPs` como `[]string`. Uma reprodução offline do decode
contra o corpo real capturado da API do Tactical no tenant de homologação
(29 agentes, 2026-09-15) confirmou `*json.UnmarshalTypeError` em
`local_ips`: a API retorna esse campo como **string** — 26 dos 29 agentes
com um único endereço, 3 com dois endereços separados por vírgula e um
espaço (ex. `"10.0.0.4, 10.0.0.5"`), 0 arrays, 0 strings vazias, 0
separados por ponto e vírgula, 0 serializados como JSON aninhado — nunca
`[]string`. Como `ListAgents`/`GetAgent` decodificam o array inteiro numa
única chamada `json.Unmarshal`, esse único campo incompatível abortava a
lista completa, derrubando `FindAgentByHostname` para **qualquer**
hostname do tenant — a causa raiz comprovada por trás de duas tentativas
reais de refresh (`match_status` permanecendo `missing_tactical` mesmo com
o agente presente e válido no Tactical) e, muito provavelmente, do
incidente original do Gate 4 também (não confirmável retroativamente —
nenhum corpo daquele momento sobreviveu).
Corrigido em duas partes: (1) novo tipo interno `flexibleLocalIPs` com
`UnmarshalJSON` próprio, usado no lugar de `[]string` nos dois DTOs — nunca
retorna erro. Aceita string única (um elemento), string separada por
vírgula (`strings.Split` + `TrimSpace` em cada parte — nunca por espaço,
confirmado como não sendo o separador real), array de strings, `null` e
string vazia (lista vazia); qualquer outro formato (number, boolean,
object, ou um array contendo um elemento não-string) produz lista vazia e
marca o campo como inválido (`Agent.LocalIPsValid=false`, espelhando
`LastSeenValid`) sem abortar o agente — `AgentID`, `Hostname`, `Status` e
os demais campos permanecem íntegros, e nenhum outro agente do array é
afetado. (2) `ListAgents`/`GetAgent` não descartam mais por completo um
erro de decode remanescente (qualquer outro campo, futuro ou não previsto
aqui): `decodeError` reconhece `*json.UnmarshalTypeError` (preserva
`Field`, o tipo Go esperado, o tipo JSON recebido — uma palavra genérica
como "string"/"number"/"bool"/"array"/"object", nunca o valor — e o
offset) e `*json.SyntaxError` (preserva só o offset), gravados em campos
novos e sanitizados de `tactical.Error` (`DecodeField`, `DecodeGoType`,
`DecodeJSONType`, `DecodeOffset`). `cmd/server` expõe isso no log
sanitizado da 7.4-R2 (`tacticalDecodeDetails`) sem nunca incluir valor,
corpo, URL, header ou API key.
Motivo: um campo populado incorretamente não pode derrubar a identificação
de um equipamento inteiro; e quando algo além de `local_ips` divergir do
schema esperado no futuro, o erro real não pode voltar a ser descartado
silenciosamente como aconteceu aqui — essa foi exatamente a razão de duas
tentativas de refresh terem sido inconclusivas antes desta investigação.
Alternativas consideradas: exigir `[]string` e apenas tolerar erro de
decode via log (rejeitada — não resolve o problema, só o torna visível;
`ListAgents` continuaria falhando para o tenant inteiro); normalizar
`local_ips` no lado do servidor Tactical (fora do controle do WACalls,
não aplicável); assumir espaço como separador adicional (rejeitada — sem
evidência real, e endereços IPv6 usam `:`, não espaço, então não há risco
de colisão a mitigar).
Consequências: `ListAgents`/`FindAgentByHostname` deixam de falhar para o
tenant inteiro por causa de um único campo com forma inesperada;
`Agent.LocalIPs`/`LocalIPsValid` seguem o mesmo padrão já estabelecido por
`LastSeenValid`; qualquer decode-erro remanescente em outro campo passa a
ser diagnosticável pelo log sanitizado sem precisar de nova investigação
offline. Testes: `internal/tactical/client_test.go`
(`TestFlexibleLocalIPs` — 12 formatos; `TestListAgentsSyntheticRealSchemaToleratesStringLocalIPs`
— fixture sintética com o schema real; `TestDecodeErrorPreservesStructuralDetailForTypeMismatch`;
`TestDecodeErrorPreservesOffsetForSyntaxError`) e
`cmd/server/supportservice_test.go`
(`TestSupportService_RefreshDeviceBinding_RealSchemaLocalIPsEndToEnd` — único
teste do arquivo que usa um `tactical.Client` real, não mock, provando
`missing_tactical → matched` com `glpi_computer_id=59` preservado através
do caminho de decode real).

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
