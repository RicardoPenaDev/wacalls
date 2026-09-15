# T-008 — Realinhamento do produto: central técnica + portal do card Windows

Status: **Etapa 1 (análise), F0 (inventário técnico do card) e Fix D-025 (persistência de vínculo) concluídos. Próxima ação técnica: F1 — Conversation Core (Fase 4A).**

Esta tarefa reorganiza a arquitetura e a interface do produto: o WACalls ServiceOps evolui para uma central integrada de suporte técnico (atendimentos, filas, chamados GLPI, equipamentos e Tactical RMM), superando o modelo de painel lateral sobre o WhatsApp.

## 1. Contexto e Estado das Entregas Anteriores

- **T-007 (MVP GLPI + Tactical):**
  - **Gates 1-4:** Concluídos e publicados em `origin/main` no commit `f883897`.
  - **Gate 5 (Homologação de Interface):** Interrompido a pedido do usuário para alinhamento da direção do produto.
  - **Painel lateral atual:** Baseline técnico provisório; não representa a experiência final esperada para o cockpit ServiceOps.
  - **Defeito de persistência automática de vínculo (D-025):** Corrigido no backend para persistir `device_binding_id` na criação e enriquecimento exato por hostname, com cobertura por testes unitários e E2E 12/12 (sem backfill em tickets passados).
  - **Homologação da interface ServiceOps:** Nenhuma homologação da interface final foi concluída.

## 2. Inventário Técnico do Card Windows (F0 — Concluído)

O bloqueio anterior por ausência do código foi superado. O código-fonte do card desktop foi localizado em diretório separado (`cardwindows/`), fora do repositório público do WACalls, e inspecionado em modo estritamente somente leitura.

### Achados Essenciais do Card

- **Stack e Arquitetura:**
  - Aplicativo desktop funcional desenvolvido em C# 12 sobre .NET 8 (`net8.0-windows`), utilizando WPF para a interface gráfica e interoperabilidade com Windows Forms (`NotifyIcon`) para operação na bandeja do sistema.
  - Processo de instância única garantido por `Mutex` global nomeado (`Global\SuporteTI_Serrana_SingleInstance`).
- **Coleta e Diagnóstico Local (Estado Atual):**
  - Coleta automática de hostname (`Environment.MachineName`).
  - Coleta de IPv4 da interface de rede ativa com filtragem de adaptadores virtuais, VPNs e loopback.
  - Leitura de patrimônio a partir do arquivo de configuração local (`config.json`) com fallback para consulta de número de série da BIOS via WMI (`Win32_BIOS`).
  - Teste contínuo de conectividade com a Internet via Ping ICMP assíncrono para host configurável (padrão `8.8.8.8`) a cada 10 segundos.
  - O card atual **não utiliza** `MachineGuid` nem `machineFingerprint()`.
- **Comunicação Atual:**
  - O botão de abertura de chamado não consome nenhuma API: delega a abertura do portal Self-Service do GLPI diretamente ao navegador padrão do sistema operacional.
  - Nenhuma requisição HTTP/HTTPS própria, nenhuma biblioteca cliente de API, nenhum listener de rede ou porta local aberta.
- **Interface e Limitações Estruturais:**
  - Janela sem bordas (`WindowStyle="None"`), com cantos arredondados, fundo translúcido e tamanho fixo de 360x470 pixels sem permissão de redimensionamento (`ResizeMode="NoResize"`).
  - A interface atual não comporta diretamente telas de formulário complexo ou histórico de mensagens.
  - **Conclusão técnica de interface:** O aplicativo pode e deve ser mantido na mesma base tecnológica (.NET 8 WPF), porém a interface exige reestruturação para uma arquitetura modular baseada em `UserControls` acionados por uma janela hospedeira capaz de expansão e reposicionamento dinâmico.
- **Segurança, Distribuição e Estado Futuro Proposto:**
  - O aplicativo não possui repositório Git próprio no momento.
  - Distribuição e atualizações dependem de instalador Inno Setup e automação via Tactical RMM; não há mecanismo de auto-update embutido.
  - O arquivo `config.json` reside em texto puro e sem proteção contra adulteração local.
  - Não há assinatura digital (Authenticode) nos executáveis e instaladores gerados.
  - **Estado futuro proposto para identidade:** O agente utilizará um identificador estável de instalação baseado no Windows/`MachineGuid`, acompanhado de uma credencial individual de dispositivo emitida pelo servidor no momento do registro da instalação, revogável e sem privilégios administrativos.
  - O identificador de instalação atua exclusivamente como chave estável de correlação cadastral, **nunca equivalendo a autenticação**.
  - A credencial será armazenada com proteção nativa do Windows, definindo-se o escopo (`CurrentUser` vs. `LocalMachine`) após confirmação do modelo de contas das estações (conta compartilhada vs. múltiplos perfis).

## 3. Decisões Arquiteturais e de Produto

### D-025 — Regra de Persistência Automática de Vínculo

Quando hostname ou patrimônio resultar em correspondência exata e única de `device_binding`, o registro em `support_requests` deve persistir obrigatoriamente `device_binding_id`. O campo `glpi_computer_id` permanece conforme o modelo atual. O `tactical_agent_id` permanece pertencendo exclusivamente ao registro de `device_bindings`, sem necessidade de duplicação em `support_requests`. Correspondências ausentes ou ambíguas nunca devem produzir associação silenciosa.

*Contexto histórico do achado:* No ticket de homologação #4, a resolução automática identificou o equipamento no GLPI e no Tactical, mas o campo `support_requests.device_binding_id` permaneceu `NULL`. A correção isolada assegura que futuras criações com correspondência exata persistam a chave estrangeira do vínculo.

### D-026 — Requisitos de Produto do Portal do Agente Windows

1. **Comportamento da janela e abertura:** O card inicia compacto no canto da tela (System Tray) e aumenta dinamicamente para se transformar na janela de chat interativo.
2. **Listagem e privacidade do histórico:** Apresenta a listagem dos chamados ativos daquela máquina com número de protocolo, título resumido e status atual. Chamados encerrados não aparecem nessa listagem. Ao entrar em um chamado ativo, a interface não exibe automaticamente as mensagens anteriores (preservando a privacidade em estações compartilhadas), mas permite o envio imediato de novas mensagens e a visualização das respostas recebidas a partir daquela retomada.
3. **Fluxo do solicitante:** O funcionário informa seu nome e telefone (opcional), escolhe entre o computador atual ou outro equipamento e fornece exclusivamente uma descrição livre do problema. O título curto do chamado é gerado automaticamente pelo sistema a partir da descrição. O solicitante não seleciona categoria (`support_requests.category_id` permanece opcional no schema, vazio nesse fluxo).
4. **Identificação de equipamento e escopo autorizado:** Os dados do computador atual são exibidos em modo somente leitura. Caso selecione "outro equipamento", a indicação é feita por digitação de hostname ou patrimônio com exigência de correspondência exata. O escopo autorizado é derivado estritamente da identidade autenticada do card e do vínculo da máquina atual com sua respectiva unidade/secretaria (e não apenas do nome informado pelo solicitante). Se o equipamento não for localizado dentro desse escopo autorizado, a abertura do chamado é bloqueada.
5. **Criação do chamado e roteamento:** O ticket no GLPI é criado imediatamente após as perguntas iniciais e o atendimento é direcionado para a fila correspondente à unidade/setor do equipamento.
6. **Sessão, anexos e notificações:** O chat pode ser fechado e retomado a qualquer momento sem perda de contexto. O envio de imagens e documentos é suportado desde a primeira versão. Mensagens recebidas com a janela fechada disparam notificação nativa do Windows e alerta visual/contador no ícone da bandeja.
7. **Contingência offline:** Se o servidor WACalls estiver indisponível, o card exibe números de telefone e WhatsApp de suporte para contingência imediata.
8. **Identidade e segurança do dispositivo:** Identificador de instalação derivado do Windows/`MachineGuid` (insuficiente como autenticação) aliado a uma credencial individual revogável sem privilégios administrativos, armazenada com proteção nativa do Windows (escopo `CurrentUser` vs. `LocalMachine` a definir).
9. **Transição de canal e encerramento:** A migração para o WhatsApp ocorre somente após oferta proativa do técnico e aceite explícito do funcionário; após o aceite, o WACalls envia automaticamente a primeira mensagem no WhatsApp. O técnico realiza o encerramento diretamente no cockpit, sincronizando o status com o GLPI e notificando o funcionário no card.
10. **Cockpit técnico ServiceOps:** Na interface técnica do WACalls, módulos de WhatsApp, ligações, Kanban, campanhas e fluxos permanecem em área secundária, priorizando chamados, filas, atendimentos e telemetria.

## 4. Fases do Realinhamento e Matriz de Dependências (T-008)

| Etapa | Escopo | Depende de | Status |
|---|---|---|---|
| **F0** | Inventário do card | — | Concluído |
| **Fix D-025** | Persistência do `device_binding_id` | — | **Concluído** |
| **F1** | Conversation Core | Fix D-025 | Próxima |
| **F2** | Filas e roteamento por unidade/setor | Contexto de equipamento existente e F1 | Pendente |
| **F3** | Interface ServiceOps do técnico | F1 e F2 | Pendente |
| **F4** | Identidade do dispositivo e portal/chat no card | F0, Fix D-025, F1, F2 e F3 | Pendente |
| **F5** | Migração consentida Portal ↔ WhatsApp | F1 e F4 | Pendente |
| **F6** | Encerramento e sincronização GLPI | F1, F3 e F4 | Pendente |

*Nota:* Não haverá fase intermediária "sem chat". O portal do card será entregue com experiência completa de chat e mensageria integrada ao Conversation Core.

## 5. Salvaguardas e Ambiente de Homologação

- O servidor de homologação anterior (porta 8085) permanece desligado e preservado.
- Os dados do ambiente de homologação (`wacalls_homolog.db`, WAL, SHM, sessão e configurações) estão integralmente intactos.
- Tickets #4 e #5 permanecem preservados no GLPI de homologação, sem backfill ou alteração retroativa.
- Nenhuma chamada externa a GLPI, Tactical ou WhatsApp deve ser executada durante testes de unidade ou integração.
