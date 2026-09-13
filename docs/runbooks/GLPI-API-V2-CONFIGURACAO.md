# GLPI High-Level REST API v2 — configuração

Guia operacional genérico para integrar o WACalls ao GLPI 11 pela High-Level
REST API v2.3. Não registrar domínio, Client ID, Client Secret, usuário, senha,
token ou dados reais neste arquivo, no Git, em chamados ou em screenshots.

## Contrato adotado

- API: GLPI High-Level REST API 2.3 sob `/api.php/v2.3`.
- Token: `POST /api.php/token`.
- Autenticação: OAuth2 password grant com scope `api`.
- Execução: backend WACalls server-to-server.
- API legada `apirest.php`: desligada.
- Banco do GLPI: nunca é interface de integração do WACalls.

O cliente OAuth existente “API Teste” não serve ao WACalls porque não possui
password grant. Criar cliente e conta técnica exclusivos.

## Ambientes

| Controle | Homologação | Produção |
|---|---|---|
| cliente OAuth | exclusivo do WACalls e separado de produção | exclusivo do WACalls |
| conta técnica | exclusiva, sem privilégio administrativo | exclusiva, sem privilégio administrativo |
| perfil e entidades | mínimos para os cenários homologados | mínimos para o escopo operacional aprovado |
| rede | allowlist/restrição de IP quando suportada | allowlist/restrição de IP obrigatória quando suportada |
| TLS | certificado válido preferencial; exceção interna documentada | certificado válido obrigatório |
| API legada | desligada | desligada |
| segredos | secret manager ou ambiente protegido | secret manager ou ambiente protegido |

Não copiar credenciais entre ambientes. Homologação e produção devem possuir
clientes OAuth, contas técnicas, senhas e ciclos de rotação independentes.

## URL base pela interface

1. Na interface administrativa do GLPI, confira a URL pública/base da instalação
   usada pelo backend. O nome do campo pode variar conforme a distribuição.
2. Corrija a URL pela própria interface administrativa e salve.
3. Use HTTPS, host canônico e o caminho base da instalação, sem credenciais,
   query string ou fragmento.
4. Configure `WACALLS_GLPI_BASE_URL` com essa base; o cliente acrescenta
   `/api.php/token` e `/api.php/v2.3`.
5. Valide que redirects não levam a outro host e que o certificado corresponde
   ao host configurado.

Não corrigir `url_base` diretamente no banco durante configuração normal.

## Cliente OAuth2 do WACalls

Na interface administrativa do GLPI:

1. Crie um cliente OAuth exclusivo e identificável como pertencente ao WACalls.
2. Habilite password grant. Desabilite grants que o WACalls não utiliza.
3. Autorize somente o scope `api`.
4. Restrinja os IPs de origem ao backend WACalls quando o GLPI oferecer esse
   controle.
5. Guarde Client ID e Client Secret apenas no secret manager ou no ambiente
   protegido do serviço.
6. Não reutilize o cliente “API Teste”; ele possui somente
   `authorization_code` e `client_credentials`.

`client_credentials` permanece limitado ao inventário e não substitui a conta
técnica necessária para criar tickets.

## Conta técnica e menor privilégio

1. Crie uma conta exclusiva, sem login compartilhado com operadores.
2. Associe perfil e entidades mínimos para:
   - consultar `Assets/Computer`;
   - criar `Assistance/Ticket`;
   - consultar apenas o necessário para validar o resultado da integração.
3. Não conceda administração geral, alteração de configuração, acesso ao banco
   ou entidades fora do escopo.
4. Use os headers contextuais `GLPI-Entity`, `GLPI-Profile` e
   `GLPI-Entity-Recursive` somente quando definidos pela operação aprovada.
5. Restrinja origem por IP quando possível e audite o uso da conta.

A ausência de rota v2.3 para Ticket↔Computer não justifica ampliar privilégios,
habilitar a API legada ou dar acesso direto ao banco.

## Variáveis do WACalls

Registrar valores somente no secret manager ou no ambiente protegido. O Git e
`.env.example` recebem apenas nomes/placeholders não secretos.

- `WACALLS_SUPPORT_ENABLED`
- `WACALLS_GLPI_BASE_URL`
- `WACALLS_GLPI_WEB_BASE_URL`
- `WACALLS_GLPI_CLIENT_ID`
- `WACALLS_GLPI_CLIENT_SECRET`
- `WACALLS_GLPI_USERNAME`
- `WACALLS_GLPI_PASSWORD`
- `WACALLS_GLPI_ENTITY_ID`
- `WACALLS_GLPI_PROFILE_ID`
- `WACALLS_GLPI_ENTITY_RECURSIVE`
- `WACALLS_GLPI_ACCEPT_LANGUAGE`
- `WACALLS_GLPI_TIMEOUT_SECONDS`

O scope `api`, o token endpoint `/api.php/token` e a base versionada
`/api.php/v2.3` são contrato do cliente, não segredos configuráveis.
TLS permanece sempre verificado; não há suporte a flags inseguras nem opção `InsecureSkipVerify`.

### Interface Web do GLPI (`WACALLS_GLPI_WEB_BASE_URL`)

- **Objetivo**: Fornece a URL base HTTPS para geração segura do link web de tickets para atendentes humanos (`{origem_web}/front/ticket.form.php?id={id}`).
- **Obrigatoriedade e Fallback Seguro**: Variável opcional. Quando não informada, a funcionalidade do botão web permanece desabilitada e o painel de suporte exibe exclusivamente a ação "Copiar número" do chamado.
- **Regras de Segurança Estritas**:
  - Exige estritamente esquema HTTPS (HTTP rejeitado incondicionalmente, inclusive em loopback/localhost).
  - Proibido conter userinfo (credenciais embutidas), query strings ou fragmentos.
  - Deve possuir a mesma origem (esquema, host normalizado e porta efetiva) de `WACALLS_GLPI_BASE_URL`; divergência de origem aborta o startup do servidor. HTTPS sem porta e HTTPS :443 são equivalentes.
  - O href interno retornado pela API v2.3 (`/api.php/v2.3/...`) é restrito ao banco de dados interno e jamais exposto ao navegador do usuário.

## Validação segura

Executar primeiro em homologação, a partir do mesmo segmento/IP do backend:

1. Confirmar High-Level REST API v2.3 habilitada e API legada desligada.
2. Confirmar URL base, HTTPS e cadeia do certificado.
3. Confirmar que o cliente exclusivo aceita password grant e scope `api`.
4. Confirmar que a conta técnica vê somente perfil e entidades autorizados.
5. Obter token por `POST /api.php/token` sem imprimir request ou response.
6. Consultar `GET /api.php/v2.3/Assets/Computer` com filtro RSQL por `name` e
   limite, usando um ativo de teste autorizado.
7. Criar ticket somente em homologação, com autorização operacional e dados de
   teste sanitizados; confirmar resposta `201` com `id` e `href`.
8. Confirmar que logs não contêm Authorization, Client Secret, usuário, senha,
   access token, refresh token ou query string.

Em produção, prefira uma verificação sem efeito colateral. Não crie ticket de
teste sem autorização explícita da operação.

## Rotação e revogação

1. Gere nova credencial no GLPI e armazene-a no secret manager.
2. Atualize o ambiente protegido do WACalls por mudança controlada.
3. Reinicie/recarregue o serviço conforme o procedimento operacional.
4. Valide autenticação e uma leitura autorizada sem registrar segredos.
5. Revogue a credencial anterior e encerre tokens/sessões associados pela
   interface do GLPI.
6. Registre data, responsável, ambiente e resultado, nunca o valor do segredo.

Em suspeita de vazamento, desabilite/revogue imediatamente cliente, segredo,
senha e tokens afetados; depois investigue logs e histórico. Não espere a janela
normal de rotação.

## Recuperação excepcional por SQL

Alteração SQL direta não é procedimento de configuração e não é integração.
Só considerar quando a interface administrativa estiver indisponível, houver
autorização explícita do responsável pelo GLPI e não existir alternativa
suportada.

Antes de qualquer alteração:

1. abrir janela de manutenção e impedir escritas concorrentes;
2. gerar backup completo e verificar que ele pode ser restaurado;
3. revisar a instrução para a versão exata do GLPI e do banco;
4. registrar plano de rollback e aprovação;
5. aplicar a menor alteração possível e validar pela interface;
6. restaurar o uso normal da interface e auditar o resultado.

O WACalls nunca recebe credenciais do banco GLPI e nunca lê ou grava suas
tabelas. Ver `docs/tasks/T-004-CLIENTES-GLPI-TACTICAL.md`, D-015 e
`docs/SECURITY.md`.
