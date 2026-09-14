# T-007 — Homologação, Hardening, E2E Permanente e Deploy da Fase 1

Status: **Planejamento detalhado concluído; execução não iniciada**.  
Baseline: commit `31a2d1bcd9d939794a1840d406ad4be0b831fe3e` (`main`).  
Contexto: WACalls ServiceOps — Fase 1 (MVP GLPI + Tactical).

---

## 1. Objetivo e Resultado Esperado

A tarefa **T-007** é o marco final de encerramento da **Fase 1 (MVP GLPI + Tactical)** do WACalls ServiceOps. O seu objetivo é consolidar a entrega do domínio de suporte técnico backend (T-005) e frontend (T-006) com:

1. **Suíte Go 100% verde**: Investigação diagnóstica e resolução determinística do teste legado de licença `TestLicenseStatusFaixasDeVencimento` (`cmd/server/license_test.go`), cobrindo timezones e limites de data sem mascaramento.
2. **Hardening do Link GLPI**: Deliberação da estratégia de navegação do ticket (considerando que o `href` da API v2.3 pode ser um recurso JSON e não interface web), validação estrita de esquema (`https:`), sanitização contra injeção de esquemas perigosos (`javascript:`, `data:`), rejeição de `userinfo`, inclusão obrigatória de `rel="noopener noreferrer"`, garantia de ausência de credenciais em URLs e comportamento seguro para links inválidos (com Opção C como fallback seguro).
3. **E2E Permanente e Reproduzível**: Substituição do script temporário por suíte automatizada dedicada (`npm --prefix client run test:e2e`), com mocks locais em loopback (GLPI 11.0.8 / Tactical RMM), validação estrita de rede (anti-duplo clique com contagem exata de requisições POST), ciclo de vida (`processing`, `synced`, `retryable_error`, `unknown`, `failed`), cancelamento de polling e segregação de permissões operador vs administrador.
4. **Protocolo de Homologação Real Controlada**: Procedimento auditado em três ambientes explicitamente separados (Homologação Pessoal de Ricardo, Homologação Institucional da Prefeitura de Serrana e Produção), utilizando credenciais isoladas em variáveis de ambiente, chamado de teste identificado (`[HOMOLOG-WACALLS-T007]`), verificação de menor privilégio e encerramento unicamente por operação oficial confirmada ou manual.
5. **Procedimento Operacional de Deploy e Rollback**: Roteiro operacional para implantação segura com ativação progressiva via feature flags (`WACALLS_SUPPORT_ENABLED`, `features.support`, `features.tactical`), migração aditiva de schema, verificação de saúde (`/healthz`) e plano de reversão imediato (kill switch).
6. **Hardening de Segurança e Conformidade**: Isolamento estrito de tenants SaaS (`tenant_id`), proteção anti-IDOR, segurança de cabeçalhos HTTP, limites de payload, trilha de auditoria e conformidade com LGPD (minimização e retenção). Estudo comparativo de rate limiting mantido em aberto para decisão futura.
7. **Critérios de Aceite Formais da Fase 1**: Checklist definitivo para declaração de conclusão da Fase 1 e transição para o roadmap da Fase 2.

> **Regra de Não-Execução**: Este documento constitui exclusivamente o **planejamento** da T-007. Nenhuma alteração em código de produção, testes ou configurações de infraestrutura deve ser executada sem autorização explícita prévia do usuário para cada etapa.

---

## 2. Detalhamento dos Eixos Técnicos

### 2.1 Investigação Diagnóstica do Teste Legado de Licença

- **Arquivo e Pacote Reais**: `cmd/server/license_test.go` e `cmd/server/license.go` (pacote `main`).
- **Sintoma Observado na Suíte Global**:
  ```text
  --- FAIL: TestLicenseStatusFaixasDeVencimento (0.15s)
      --- FAIL: TestLicenseStatusFaixasDeVencimento/com_folga: diasParaVencer = 19, esperado 20
      --- FAIL: TestLicenseStatusFaixasDeVencimento/vence_em_2_dias: diasParaVencer = 1, esperado 2
  ```
- **Metodologia de Diagnóstico e Contrato Técnico**:
  Em vez de antecipar suposições sobre a causa antes da análise técnica aprofundada, a etapa 7.1 deve seguir rigorosamente os seguintes passos de investigação:
  1. **Reprodução em UTC**: Executar a suíte forçando `TZ=UTC` para isolar interferências de fuso horário.
  2. **Reprodução em America/Sao_Paulo**: Executar forçando `TZ=America/Sao_Paulo` para verificar o impacto de horários de verão legados ou deslocamento de fuso (UTC-3).
  3. **Comportamento em Limites de Data**: Testar execuções próximas da meia-noite (23:59 vs 00:01) e transições de virada de dia.
  4. **Identificação da Causa Raiz**: Avaliar se a divergência decorre de chamadas concorrentes a `time.Now()`, timezone local (`time.Local`) versus `time.UTC`, truncamento por divisão inteira de segundos (`/ 86400`), cálculo de duração de tempo versus cálculo de calendário civil, ou premissa/expectativa incorreta na tabela de testes.
  5. **Correção Determinística**: Ajustar a implementação para que seja matematicamente consistente e reprodutível em qualquer horário, fuso ou sistema operacional.
  6. **Proibições Estritas**: É proibido utilizar `time.Sleep()`, `t.Skip()`, tolerâncias arbitrárias (`assert abs(diff) <= 1`) ou mascaramentos.
  7. **Validação de Estabilidade**: Executar o pacote `cmd/server` repetidamente (ex.: `go test -count=10 -run TestLicenseStatusFaixasDeVencimento ./cmd/server`) e, em seguida, validar a suíte completa com `go test ./...`.

---

### 2.2 Suíte de Testes E2E Permanente e Reproduzível

- **Comando Dedicado**:
  ```powershell
  npm --prefix client run test:e2e
  ```
- **Separação de Testes**:
  - `npm --prefix client run test`: Mantido estritamente para testes unitários rápidos e contratos em memória (`node --test tests/**/*.test.mjs`, execução em milissegundos sem navegador).
  - `npm --prefix client run test:e2e`: Comando dedicado para validação ponta a ponta com Playwright (`client/tests/e2e/run.mjs`), orquestrando mocks locais em loopback, backend Go e preview Vite da SPA compilada.
- **Modo E2E (`-e2e-mode`) e Salvaguardas Obrigatórias**:
  1. **Flag Explícita `-e2e-mode`**: Desativada por padrão. **NUNCA deve ser usada em produção**. Quando desativada, o comportamento de produção permanece rigorosamente inalterado (nenhuma rota de teste, nenhuma sessão sintética, e flags de seed causam aborto imediato de inicialização).
  2. **Condições Rígidas para Ativação**:
     - Servidor escutando estritamente em loopback (`127.0.0.1`, `localhost`, `[::1]`; bind universal `:port`, `0.0.0.0`, `[::]:port` ou `::` é estritamente rejeitado);
     - Banco SQLite fornecido explicitamente pelo runner e com diretório de execução `-e2e-run-dir` obrigatório;
     - `runDir` canônico deve estar estritamente contido em `os.TempDir()` e possuir prefixo exclusivo `wacalls-e2e-`;
     - Arquivo de banco SQLite deve estar diretamente dentro de `runDir` com prefixo exclusivo `wacalls-e2e-` e extensão `.db`;
     - Rejeição estrita de banco preexistente, traversal (`../`), diretório irmão com prefixo semelhante, symlink/junction fora do `runDir` e banco de produção padrão `wacalls.db`;
     - `WACALLS_SUPPORT_ENABLED` ativo;
     - `WACALLS_GLPI_BASE_URL`, `WACALLS_GLPI_WEB_BASE_URL` e `WACALLS_TACTICAL_BASE_URL` apontando estritamente para loopback;
     - Nenhuma URL externa configurada; qualquer violação aborta o boot imediatamente sem registrar sessão nem tentar tráfego de rede.
  3. **Sessão WhatsApp Sintética Estritamente em Memória**:
     - Criada exclusivamente quando `-e2e-mode` e `-e2e-session-id` estão ativos;
     - Registrada em memória via `SessionManager.register()`;
     - **Nunca** chama `Device.Save()`, `PutDevice()` ou `startPairing()`;
     - **Nunca** é persistida no banco SQLite WhatsApp (`sessions` ou tabelas do whatsmeow);
     - Utiliza apenas JIDs e nomes sintéticos;
     - Fornece metadados mínimos para a conversa aparecer e abrir no frontend (`messages` e `chat_meta`);
     - Flags discretas sem separadores perigosos: `-e2e-run-dir`, `-e2e-session-id`, `-e2e-session-name`, `-e2e-own-jid`, `-e2e-chat-jid`, `-e2e-chat-name`, `-e2e-hostname`, `-e2e-glpi-computer-id`, `-e2e-tactical-agent-id`.
  4. **Restrição de `WACALLS_GLPI_CA_FILE`**:
     - Permite que o backend confie no certificado autoassinado do mock local de GLPI;
     - TLS permanece **100% verificado** (`InsecureSkipVerify: false`);
     - Restrita exclusivamente ao `-e2e-mode` (se informada fora do modo E2E, o startup falha);
     - Leitura segura em PEM limitada a 1 MiB com mensagens de erro sanitizadas que não expõem caminhos sensíveis ou conteúdo do arquivo.
  5. **Isolamento de Rede e Seus Limites**:
     - O `networkGuard` do Playwright intercepta todo o tráfego do navegador, abortando e falhando o teste caso qualquer requisição tente sair de `127.0.0.1`/`localhost`/`::1`;
     - WebSocket não-loopback é interceptado e bloqueado via `context.routeWebSocket()`;
     - Service Workers são desativados via `serviceWorkers: "block"`;
     - A dependência externa de flags (`flagcdn.com`) em `LanguageSwitcher` foi removida, tornando o frontend 100% offline;
     - O orchestrator Node sanitiza as variáveis de ambiente (sem herança de `WACALLS_*` do shell) e valida URLs de loopback antes do spawn;
     - Mocks escutam exclusivamente em `127.0.0.1`;
     - **Limite do Isolamento**: Este isolamento atua no nível de aplicação, interceptação de navegador e configuração de processos. Não constitui uma jaula/sandbox em nível de kernel (como namespaces Linux ou container Docker isolado sem rede).
  6. **Contrato de Retry-After e Mapeamento de Status HTTP**:
     - A API pública mantém o contrato fixo de 60 segundos aprovado na T-006 exclusivamente para erros `rate_limited` (`Retry-After: 60`), derivado de `updatedAt` persistido no banco para sobreviver a recargas de página;
     - O valor dinâmico de `RetryAfter` retornado internamente pelo cliente GLPI (`internal/glpi`) fica registrado como débito técnico de hardening futuro;
     - Erros com `integration_auth_failed` retornam HTTP 502 Bad Gateway com o envelope `SupportTicketResponseEnvelope` preservado para que o frontend renderize o estado recuperável adequadamente;
     - Timestamps Unix em segundos do backend são convertidos com segurança para milissegundos via helper `supportDate.ts`, tratando zero, nulo, negativo e inválido sem exibir 1970.
- **Cobertura da Matriz de Estados e Cenários**:
  1. **Anti-Duplo Clique & Idempotência**: Validação por interceptação de rede confirmando que cliques múltiplos e rápidos no formulário disparam exatamente uma única requisição POST (`postCount === 1`) com header `Idempotency-Key` válido (`ui-...`).
  2. **Caminho Feliz (`synced`)**: Transição para "Sincronizado", renderização de `#1000`, link web sanitizado para o GLPI com `target="_blank"` e `rel="noopener noreferrer"`, e timestamp no período atual.
  3. **Falha Recuperável (`retryable_error`)**: Simulação de HTTP 429 com `Retry-After: 2`, bloqueio temporário do botão, reenvio após countdown e sucesso na sincronização.
  4. **Falha Ambígua e Segregação de Permissões (`unknown`)**: Simulação de 503 no GLPI, verificação de que o operador comum não visualiza ação de conciliação administrativa e confirmação de que o administrador executa a conciliação manual via `ReconcileDialog`.
  5. **Falha Permanente (`failed`)**: Simulação de 400 Bad Request, exibição do status "Falha Permanente" e botão "Criar Novo Chamado" reabrindo o formulário limpo.
  6. **Cancelamento de Polling**: Verificação de que requisições periódicas cessam quando o painel de suporte é fechado e quando ocorre troca de conversa ativa.
  7. **Política de Artefatos**: Relatórios e traces gravados em `client/test-results/` (ignorado pelo Git). Limpeza garantida de processos órfãos (`SIGTERM`/`SIGKILL`) e diretórios de estado temporário.

---

### 2.3 Hardening do Link GLPI ("Abrir no GLPI")

- **Componentes e Arquivos Reais**:
  - Frontend: `client/src/components/domain/support/SupportRequestStatus.tsx`, `client/src/types/support.ts`.
  - Backend: `cmd/server/supportservice.go`, `cmd/server/supportapi.go`, `internal/glpi/client.go`.
- **Ressalva Arquitetural do Href da API**:
  O `href` retornado pela High-Level REST API v2.3 do GLPI aponta tipicamente para um endpoint REST de recurso JSON (ex.: `/api.php/v2.3/Assistance/Ticket/1001`), e **não** para a interface gráfica web utilizada por atendentes humanos (ex.: `/front/ticket.form.php?id=1001`). Portanto, o `href` da API não deve ser renderizado diretamente como link de navegação visual sem confirmação de que ele realmente entrega uma interface humana adequada.
- **Decisão Arquitetural Resolvida (Decisão D-020 — Opção A Restrita)**:
  O link web é construído e validado exclusivamente pelo backend a partir de `WACALLS_GLPI_WEB_BASE_URL` e do ID decimal positivo do ticket (`/front/ticket.form.php?id={id}`). O `href` retornado pela API v2.3 permanece estritamente no armazenamento interno/store e é omitido do DTO público (`SupportRequestPublicDTO`), que expõe exclusivamente `webUrl` segura. Se `WACALLS_GLPI_WEB_BASE_URL` estiver ausente, `webUrl` é nulo e a interface web renderiza apenas a ação "Copiar número".
- **Requisitos Obrigatórios Atendidos**:
  1. **Esquema Seguro**: Exigência estrita de esquema `https:` (HTTP rejeitado incondicionalmente, inclusive em loopback/localhost).
  2. **Isolamento de Janela**: Presença obrigatória de `target="_blank"` e `rel="noopener noreferrer"` no link visual do frontend.
  3. **Rejeição de Esquemas e Conteúdos Perigosos**: `sanitizeGLPIWebUrl` (frontend) exige protocolo `https:` exato, pathname exato `/front/ticket.form.php`, hash/fragmento vazio e exatamente um único parâmetro de query chamado `id` com valor decimal positivo canônico; rejeita qualquer esquema diferente de `https:` (incluindo `javascript:`, `data:`, `vbscript:`), fragmentos, parâmetros de query adicionais (mesmo vazios), `id` duplicado e credenciais embutidas (`userinfo`).
  4. **Sem Credenciais ou Tokens**: Abertura por navegação limpa, sem expor tokens OAuth (`access_token`), chaves de API, senhas ou dados sensíveis em query strings ou fragmentos.
  5. **Comportamento Seguro para Href Inválido**: Caso `webUrl` seja nula, vazia, malformada ou falhe na validação defensiva do frontend (`sanitizeGLPIWebUrl`), o elemento de link não é renderizado, exibindo com segurança a identificação numérica e o botão "Copiar número".
  6. **Sem Domínios Hardcoded**: Nenhum domínio específico de homologação ou de clientes/prefeituras é fixado no código-fonte.
  7. **Alinhamento de Origem**: A origem de `WACALLS_GLPI_WEB_BASE_URL` (esquema, host e porta efetiva) deve coincidir estritamente com a de `WACALLS_GLPI_BASE_URL`, abortando no startup em caso de divergência. HTTPS sem porta e HTTPS :443 são equivalentes.

---

### 2.4 Protocolo de Homologação Real Controlada

- **Segregação Rigorosa de Três Ambientes**:
  1. **Homologação Pessoal/Controlada**:
     - Instâncias de GLPI de teste e Tactical de teste de Ricardo;
     - Primeira homologação real da T-007;
     - Utilização exclusiva de dados sintéticos e fictícios;
     - Autorizada estritamente no momento em que o usuário der o comando de execução.
  2. **Prefeitura de Serrana — Homologação Institucional**:
     - Ambiente completamente separado;
     - URLs, clientes OAuth2, usuários técnicos, perfis, permissões e API keys próprios;
     - Nenhuma reutilização automática das credenciais do ambiente pessoal;
     - Exige nova autorização formal e explícita do usuário;
     - A ser executada somente após a homologação pessoal ter sido aprovada com sucesso.
  3. **Produção**:
     - Ativação operacional posterior definitiva;
     - Exige backup completo, janela de manutenção programada, plano de rollback comprovado e autorização própria;
     - Nunca fazer ativação em produção como parte automática do processo de homologação.
- **Regras Operacionais e Protocolo de Limpeza**:
  - **Isolamento de Segredos**: Credenciais (`WACALLS_GLPI_*`, `WACALLS_TACTICAL_*`) injetadas exclusivamente via variáveis de ambiente protegidas ou secret manager. A existência de credenciais configuradas **não** constitui autorização tácita de uso.
  - **Identificação Unívoca**: Todo chamado aberto em homologação deve conter no título o prefixo obrigatório `[HOMOLOG-WACALLS-T007] <timestamp>`.
  - **Conectividade e Menor Privilégio**: Testes prévios somente de leitura (obtenção de token OAuth2 password grant com scope `api`, consulta a ativo de teste por hostname e consulta a agente no Tactical RMM) antes de disparar qualquer escrita.
  - **Protocolo de Encerramento e Limpeza**:
    - Identificar formalmente o ticket criado com marcador unívoco de homologação;
    - Confirmar no OpenAPI / especificação oficial do GLPI qual operação oficial suportada permite encerrar, cancelar ou excluir o ticket;
    - Utilizar **somente** operação oficialmente suportada e autorizada pela API;
    - Se não houver operação oficial confirmada na API, registrar o ticket formalmente para encerramento manual na interface administrativa do GLPI;
    - **Proibição Absoluta**: Nunca limpar diretamente pelo banco de dados do GLPI via comandos SQL diretos.

---

### 2.5 Procedimento Operacional de Deploy, Ativação Progressiva e Rollback

- **Matriz de Configuração e Parâmetros de Deploy**:
  | Variável | Tipo | Descrição | Valor Seguro Inicial |
  |---|---|---|---|
  | `WACALLS_SUPPORT_ENABLED` | bool | Habilita o subsistema backend de suporte | `false` |
  | `WACALLS_SUPPORT_CREATE_TIMEOUT_SECONDS` | int | Timeout da criação síncrona de ticket GLPI (mínimo 30s) | `120` |
  | `WACALLS_SUPPORT_RECOVERY_MARGIN_SECONDS` | int | Margem de expiração para recuperação de órfãos (mínimo 15s) | `30` |
  | `WACALLS_GLPI_BASE_URL` | string | URL base HTTPS da instalação GLPI 11 | Definida no secret manager |
  | `WACALLS_GLPI_CLIENT_ID` | string | Client ID OAuth2 exclusivo do WACalls | Definida no secret manager |
  | `WACALLS_GLPI_CLIENT_SECRET` | string | Client Secret OAuth2 do WACalls | Definida no secret manager |
  | `WACALLS_GLPI_USERNAME` | string | Conta técnica exclusiva de serviço | Definida no secret manager |
  | `WACALLS_GLPI_PASSWORD` | string | Senha da conta técnica | Definida no secret manager |
  | `WACALLS_GLPI_WEB_BASE_URL` | string | URL base HTTPS da interface web GLPI (opcional) | Vazio (desabilitado) |
  | `WACALLS_TACTICAL_BASE_URL` | string | URL base HTTPS da API Tactical RMM | Definida no secret manager |
  | `WACALLS_TACTICAL_API_KEY` | string | Chave de API Tactical RMM | Definida no secret manager |
  | `features.support` | bool | Feature flag frontend no cliente | `false` |
  | `features.tactical` | bool | Feature flag frontend para telemetria | `false` |

- **Roteiro de Deploy Controlado**:
  1. **Backup Pré-Deploy**: Gerar cópia consistente do banco de dados SQLite de produção (`sqlite3 wacalls.db ".backup 'wacalls_backup_pre_t007.db'"`).
  2. **Instalação do Artefato**: Aplicar o binário compilado com as flags de suporte desabilitadas.
  3. **Migração Aditiva de Schema**: O servidor WACalls aplica automaticamente a criação das tabelas `support_requests` e `support_request_events` no boot, sem bloqueio de tabelas existentes.
  4. **Health Check do Sistema**: Validar o endpoint `/healthz` e verificar se a operação normal do WhatsApp permanece íntegra.
  5. **Ativação Gradual (Canary / Feature Flags)**:
     - Habilitar `WACALLS_SUPPORT_ENABLED=true` no backend.
     - Habilitar `features.support=true` no frontend inicialmente para perfil de supervisores/administradores.
     - Validar logs sem erros.
     - Habilitar `features.support=true` para todos os atendentes.
     - Habilitar `features.tactical=true` caso a instância Tactical esteja estável; do contrário, manter desabilitada utilizando o fallback seguro.
- **Procedimento de Rollback**:
  - **Kill Switch Imediato**: Redefinir `WACALLS_SUPPORT_ENABLED=false` e `features.support=false` no ambiente e reiniciar o serviço. O painel lateral desaparece da interface e as rotas retornam 404, sem perda de conversas de chat.
  - **Rollback de Artefato**: Reverter o binário para a versão anterior de release.
  - **Rollback de Dados (em caso extremo de corrupção)**: Restaurar o arquivo de backup pré-deploy.

---

### 2.6 Governança de Segurança, Auditoria, LGPD e Estudo de Rate Limiting

- **Isolamento Multi-Tenant e Anti-IDOR**:
  - Todo acesso aos endpoints de suporte extrai e valida o `tenant_id` a partir de `currentUser.TenantID()`.
  - Consultas e mutações utilizam filtros compostos com validação de ownership (`WHERE id = ? AND tenant_id = ?`). Acessos não autorizados entre tenants retornam `404 Not Found`.
- **Proteção XSS e Limites de Payload**:
  - Frontend renderiza propriedades de dispositivos e chamados exclusivamente via nós de texto do React, sem HTML bruto.
  - Backend limita o tamanho do corpo via `http.MaxBytesReader` e valida limites de campos (`title` <= 200 caracteres, `description` <= 8.000 bytes, `requesterName` <= 120 caracteres).
- **Proteção CORS e Cabeçalhos HTTP**:
  - Header `Idempotency-Key` adicionado à allowlist do CORS estrito sem abertura de novas origens cruzadas.
  - Headers de segurança mantidos: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin`.
- **Trilha de Auditoria e LGPD**:
  - A tabela `support_request_events` armazena histórico append-only de transições de estado com timestamps e identificação do ator.
  - Minimização de dados: dados de suporte contêm apenas o essencial para a resolução do ticket; logs mascaram tokens, credenciais e chaves.
- **Questão em Aberto — Rate Limiting (Estudo Comparativo)**:
  A implementação de um rate limiter de chamados não será realizada sem aprovação prévia de decisão durável. Na Etapa 7.5, serão analisadas e comparadas as seguintes abordagens:
  - *Reverse Proxy*: Implementação na borda (Nginx, Traefik, Caddy) limitando requisições por IP ou rota.
  - *Middleware Persistente / Distribuído*: Mecanismo via Redis (token bucket ou sliding window) voltado a cenários de múltiplas instâncias futuras.
  - *Proteção Atual por Idempotency-Key + CAS*: A proteção vigente contra cliques concorrentes no client associada ao claim atômico com CAS no banco.
  - *Implicações da Arquitetura Single-Instance Atual*: Avaliação sobre o custo e complexidade de adicionar controle in-memory em processo único sob a decisão D-017.
  - *Impacto Operacional*: Avaliação do risco de bloqueio indevido de atendentes legítimos que necessitam abrir chamados em sequência ou tratar erros com 429 (`Retry-After`).

---

### 2.7 Critérios de Aceite Formais da Fase 1

A Fase 1 (MVP GLPI + Tactical) será considerada formalmente concluída somente quando todos os critérios a seguir forem plenamente atendidos:

1. [x] **Teste de Licença / Timezone Estável**: Diagnóstico realizado e teste `TestLicenseStatusFaixasDeVencimento` corrigido deterministicamente sem skips ou sleeps (suíte Go global 100% verde).
2. [x] **Build Go Global Aprovado**: `go build ./...` executado com sucesso e zero erros.
3. [x] **Suíte Go Global 100% Verde**: `go test -count=1 ./...` executado com aprovação integral em todos os pacotes.
4. [x] **Frontend Build e Testes Aprovados**: `npm run build` e `npm run test` no client executados com zero falhas (16/16).
5. [x] **Suíte E2E Permanente e Reproduzível**: `npm --prefix client run test:e2e` aprovada 2x consecutivas (12/12), com validação de rede real (anti-duplo clique com 1 único POST), matriz de 11/11 estados com assertion no navegador, e zero processos/diretórios órfãos comprovado inclusive sob falha controlada injetada em 3 estágios do boot.
6. [ ] **Conformidade Multi-Database**: Stores validados tanto em SQLite quanto no harness MariaDB 11.4 descartável (caso haja qualquer alteração de persistência).
7. [ ] **Segurança do Link GLPI Resolvida**: Decisão arquitetural de navegação implementada e validada com esquemas seguros e proteção contra XSS/open redirect (ou Opção C confirmada como fallback seguro).
8. [ ] **Homologação Real Concluída**: Teste assistido no ambiente de homologação pessoal de Ricardo executado com evidências documentadas e encerramento oficial/manual comprovado.
9. [ ] **Prontidão de Deploy e Rollback Testada**: Procedimento de ativação por feature flag e rollback emergencial comprovados operacionalmente.
10. [ ] **Documentação e Evidências Sincronizadas**: `docs/STATUS.md` e `docs/ROADMAP.md` atualizados refletindo as evidências reais de conclusão.

---

## 3. Divisão da T-007 em Etapas e Tabela de Autorizações

A execução da T-007 será realizada rigorosamente segundo o fatiamento abaixo, com governança explícita sobre a necessidade de autorização prévia do usuário:

| Etapa | Escopo Técnico | Arquivos Reais Envolvidos | Testes & Comandos de Validação | Checkpoint em STATUS.md | Autorização Prévia do Usuário |
|---|---|---|---|---|---|
| **7.1** | **Diagnóstico e Correção do Teste de Licença**: Concluída. Clock controlável injetado via `licenseNow`, testes de borda/virada/fuso adicionados; suíte Go 100% verde | `cmd/server/license.go`, `cmd/server/license_test.go` | `go test -run '^TestLicense' -count=50` e `go test ./...` | Suíte Go global 100% verde | **Concluída (autorizada)** |
| **7.2** | **Hardening do Link GLPI**: Deliberação da decisão pendente (Opção A, B ou Opção C fallback seguro), validação de esquema (`https:`), sanitização, `rel="noopener noreferrer"` e tratamento seguro para links inválidos | `client/src/components/domain/support/SupportRequestStatus.tsx`, `client/src/types/support.ts`, `client/tests/support.test.mjs`, `cmd/server/supportservice.go`, `cmd/server/supportapi.go` | `npm --prefix client run test` e `npm --prefix client run build` | Hardening do link GLPI concluído e testado | **Exige autorização prévia antes de editar arquivos** |
| **7.3** | **Suíte E2E Permanente**: implementada, auditada (achados críticos/importantes corrigidos: `cmd/e2eseed` removido, `validateE2EPreconditions` unificado via `precheckE2EBoot`, `bootEnvironment` com try/catch/finally real e taskkill/process-group corretos, matriz 11/11, CI criada, testes unitários dos fixes de produção, CA file limitada a 1 MiB, WebSocket/ServiceWorker fechados no networkGuard) | `client/tests/e2e/**`, `cmd/server/e2e_mode.go`, `cmd/server/main.go`, `cmd/server/server.go`, `cmd/server/support_config.go`, `.github/workflows/e2e.yml` | `npm --prefix client run test:e2e` (2x consecutivas, 12/12) + bateria Go/npm completa | Suíte E2E permanente versionada, corrigida e aprovada; aguardando nova auditoria antes do push | **Corrigida sob autorização explícita do usuário; push ainda não autorizado** |
| **7.4** | **Homologação Real Controlada**: Execução assistida no ambiente de teste pessoal de Ricardo com dados sintéticos, chamado identificado `[HOMOLOG-WACALLS-T007]` e encerramento via operação oficial ou registro manual | Procedimento operacional (sem alteração de código) | Chamadas controladas via backend WACalls no ambiente de teste pessoal | Homologação pessoal concluída com evidências | **Autorização obrigatória imediatamente antes de qualquer chamada real** |
| **7.5** | **Hardening de Segurança e LGPD**: Auditoria read-only de IDOR, limites de body, CORS, trilha de auditoria e estudo comparativo de rate limiting (sem aprovação prévia) | `cmd/server/supportapi.go`, `cmd/server/server.go`, `docs/SECURITY.md` | `go test -run Support ./cmd/server` | Auditoria de segurança e LGPD concluídas | **Auditoria read-only pode ser automática; correções exigem autorização** |
| **7.6** | **Deploy, Ativação Progressiva e Rollback**: Validação de flags seguras (`WACALLS_SUPPORT_ENABLED=false`, `features.support=false`), backup prévio, migração aditiva e kill switch | `docs/runbooks/DEPLOY-FASE-1.md` (se aplicável) | Smoke tests de subida do servidor e reversão emergencial | Roteiro de deploy e rollback testados | **Autorização obrigatória imediatamente antes de qualquer ação** |
| **7.7** | **Fechamento e Aceite da Fase 1**: Verificação integral do checklist de critérios de aceite, atualização de `docs/STATUS.md`, `docs/ROADMAP.md` e encerramento da Fase 1 | `docs/STATUS.md`, `docs/ROADMAP.md`, `docs/tasks/T-007-HOMOLOGACAO-HARDENING-DEPLOY.md` | Inspeção final do repositório (`git status`) | Fase 1 formalmente concluída | **Exige autorização prévia após apresentação das evidências** |

---

## 4. Decisões Arquiteturais Pendentes

As seguintes questões técnicas estão identificadas como **pendentes** e devem ser deliberadas antes ou durante as etapas correspondentes:

1. **Decisão Pendente — Estratégia de Navegação do Link GLPI (Etapa 7.2)**:
   - *Ressalva da API*: O `href` da API v2.3 pode apontar para um recurso JSON da API (ex.: `/api.php/v2.3/Assistance/Ticket/1001`), e não necessariamente para a tela web do chamado. Não utilizar o `href` da API como link visual sem confirmar que ele abre uma interface adequada.
   - *Opção A*: Backend retorna URL absoluta validada contra `WACALLS_GLPI_BASE_URL`.
   - *Opção B*: Backend retorna apenas o ticket ID e o frontend resolve a base institucional segura.
   - *Opção C (Fallback Seguro)*: Remover temporariamente "Abrir no GLPI" e manter apenas a exibição do identificador numérico textual `#<id>` com botão de cópia. Até confirmação da URL oficial da interface web, a Opção C é o fallback seguro.
2. **Questão em Aberto — Arquitetura de Rate Limiting para Criação de Chamados (Etapa 7.5)**:
   - *Status*: A Proposta D-019 permanece em aberto como estudo comparativo (Reverse Proxy vs Middleware Persistente vs Idempotency-Key/CAS atual vs Impacto Single-Instance), sem implementação ou aprovação prévia nesta fase.

---

## 5. Próximo Passo Imediato

1. Sincronizar o [docs/STATUS.md](file:///D:/fabrica/WaCalls/wacalls/docs/STATUS.md) mantendo estritamente menos de 150 linhas.
2. Incorporar as alterações no mesmo commit local via `git commit --amend --no-edit`.
3. Aguardar autorização explícita do usuário antes de iniciar a **Etapa 7.1** (investigação diagnóstica do teste de licença).
