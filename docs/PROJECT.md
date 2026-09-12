# WACalls ServiceOps

Nome oficial: **WACalls ServiceOps**
Descrição: Central unificada de atendimento, chamados e gestão de dispositivos.
Repositório oficial: <https://github.com/RicardoPenaDev/wacalls>

## Visão

Criar uma central unificada de atendimento, chamados e gestão de dispositivos
para toda a **Prefeitura de Serrana**, conectando conversa, chamado e
equipamento na mesma experiência.

Estado de implantação: os agentes Tactical estão instalados **somente na
Secretaria da Saúde** neste momento (Client `Secretaria De Saude`, ~127 agentes,
~25 sites). A expansão para as demais secretarias será gradual.

## Problema

Hoje o atendimento pode exigir alternância entre WhatsApp, GLPI e Tactical RMM.
O solicitante ainda informa manualmente dados que o agente instalado no
computador já conhece.

## Proposta

O usuário abre o suporte pelo agente Windows. O portal coleta o nome, identifica
se o problema é no computador atual ou em outro equipamento e recebe a
descrição. O WACalls centraliza a conversa e apresenta o contexto do GLPI e
Tactical ao técnico.

Cada computador exibirá seu hostname no card do agente e em uma etiqueta física
no gabinete. Quando o solicitante informar esse código, o ServiceOps localizará
o equipamento e criará o chamado GLPI já associado ao ativo e ao setor
correspondentes.

## Responsabilidade de cada sistema

| Sistema | Responsabilidade |
|---|---|
| WACalls ServiceOps | Cockpit do técnico: conversas, filas, triagem e atendimento |
| GLPI | Fonte oficial de tickets, SLA, categorias, locais, solicitantes, ativos e histórico formal |
| Tactical RMM | Fonte operacional: status do endpoint, inventário, scripts e acesso remoto |
| Agente Windows | Identificação do computador e atalho para abrir suporte |
| WhatsApp principal | Canal humano de atendimento |
| WhatsApp secundário | Automações e alertas, sem misturar com a fila humana |

O WACalls ServiceOps **não** deve tentar substituir GLPI ou Tactical.

## Usuários

- Funcionário solicitante.
- Técnico de TI.
- Administrador da plataforma.
- Gestor que consulta indicadores e auditoria.

## Princípios

- O usuário não precisa conhecer GLPI ou Tactical.
- O técnico trabalha prioritariamente no WACalls.
- O dado oficial permanece no sistema especialista.
- Toda ação técnica sensível exige intenção explícita e auditoria.
- O atendimento continua possível se uma integração externa estiver
  temporariamente indisponível.
- A expansão é gradual: a Saúde é a implantação inicial, não o limite do produto.

## Estrutura organizacional (fonte: Tactical)

```text
Prefeitura
└── Client = Secretaria
    └── Site = Unidade, departamento, UBS, escola ou setor físico
        └── Agent = Equipamento
```

O setor funcional do equipamento é identificado pelo padrão do hostname
(ver `docs/HOSTNAMES.md`). Na interface, `Client`, `Site` e `Agent` aparecem
como `Secretaria`, `Unidade` e `Equipamento`.

Nota sobre multitenancy: o código já possui um conceito de **tenant = empresa**
(SaaS). Uma secretaria **não** é um tenant; a hierarquia de secretarias vem do
Tactical. Ver `docs/DECISIONS.md` (D-009).

## Fora do escopo inicial

- Substituir integralmente GLPI ou Tactical.
- Patch management, EDR, billing, MDM ou backup.
- Diagnóstico e correção autônoma por IA.
- Sincronização bidirecional de todos os recursos dos sistemas.
- Aplicativo móvel próprio.

## Métricas iniciais

- Percentual de chamados criados com equipamento identificado.
- Tempo entre abertura e primeiro atendimento.
- Tempo para iniciar acesso remoto.
- Taxa de falha na criação/sincronização do ticket.
- Chamados encerrados no WACalls que ficaram abertos no GLPI.
