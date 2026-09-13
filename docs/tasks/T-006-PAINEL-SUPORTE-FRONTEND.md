# T-006 — Painel de Suporte GLPI + Tactical no Frontend

Status: **aprovado pelo usuário (Opção 1: Sheet lateral); baseline documental commit `a8e19f4fe0d49f5ca84f9db02eb0b7b720afbe6a`; em implementação**.

Esta especificação define exclusivamente a interface de usuário (UI/UX) do domínio de suporte técnico (GLPI + Tactical RMM) dentro do cockpit de conversas WhatsApp do WACalls ServiceOps, consumindo os endpoints internos publicados na T-005.

> **Princípio de Integridade**: O frontend nunca se comunica diretamente com o GLPI ou Tactical RMM; todas as operações trafegam exclusivamente pela API interna do WACalls. Não há dashboard geral nesta tarefa (postergada para evolução posterior). O código de produção (Go/TS/TSX/CSS) e os testes frontend são implementados em commit próprio da T-006, sem realizar push até autorização formal.

---

## A. Objetivo do MVP Visual

O atendente (operador de suporte) abre uma conversa WhatsApp com um solicitante `(session_id, chat_jid)` e consegue, sem sair da tela de atendimento:

1. **Identificar o equipamento**: Visualizar o dispositivo associado à conversa ou o último snapshot vinculado.
2. **Consultar dados de contexto**:
   - Hostname canônico (ex.: `SDE-ARS-RCP-02`) e normalizado.
   - Secretaria / Unidade / Setor inferidos pela convenção (`docs/HOSTNAMES.md`) e Tactical (`tactical_client_id` / `tactical_site_id`).
   - Patrimônio quando cadastrado.
3. **Verificar status operacional do Tactical**:
   - Quando configurado e online: status do agente, SO, IP local, último check-in.
   - Quando não configurado: capacidade ocultada graciosamente.
   - Quando indisponível: aviso amigável sem bloqueio de chamado.
4. **Abrir chamado no GLPI**:
   - Formulário validado com campos essenciais (solicitante, título, descrição, prioridade, equipamento, categoria e localização opcionais).
   - Envio idempotente com proteção contra duplo clique.
5. **Acompanhar o ciclo de vida do chamado**:
   - Exibição em tempo real do estado de sincronização (`processing`, `synced`, `retryable_error`, `unknown`, `failed`).
   - Acesso seguro ao link oficial do ticket no GLPI (`glpiTicketHref`).
6. **Trocar o equipamento associado**:
   - Buscar outros computadores do mesmo tenant cadastrados no `device_bindings`.
   - Atualizar a vinculação com auditoria transacional imediata.
7. **Executar Retry (atendente)**:
   - Acionamento manual de nova tentativa quando em `retryable_error`.
8. **Visualizar orientações seguras em `unknown`**:
   - Alerta explicativo de timeout/ambiguidade para evitar abertura duplicada manual.
9. **Reconciliação segura (administrador)**:
   - Acesso a ações administrativas (`synced`, `safe_to_retry`, `processing_orphaned`) visíveis e executáveis exclusivamente para usuários com `currentUser.IsAdmin()`.

---

## B. Propostas de Layout

Para integrar a experiência sem redesenhar o WACalls nem prejudicar a troca de mensagens, comparam-se duas opções:

### Opção 1: Painel Lateral (Drawer / Sheet deslizante à direita)

- **Descrição**: Um botão com ícone de suporte (`Headset` ou `Wrench`) no cabeçalho do `ChatView` abre um `Sheet` (drawer lateral deslizante da direita, já padronizado via `@/components/ui/sheet` como em `ContactDetailsPanel`). O painel sobrepõe ou empurra lateralmente a área da conversa.
- **Desktop (>= 1024px)**: Drawer com largura fixa de 420px–460px sobreposto à direita com backdrop leve ou como coluna lateral acoplada (sem fechar a thread).
- **Tablet (768px – 1023px)**: Drawer lateral sobreposto ocupando 50%–60% da tela.
- **Mobile (< 768px)**: Drawer ocupando 100% da viewport em tela cheia com botão de fechar proeminente.
- **Impacto no espaço das mensagens**: Zero impacto quando fechado; quando aberto em desktop como overlay, mantém as mensagens visíveis sob o backdrop; se dockable, reduz temporariamente a largura da thread.
- **Complexidade**: Baixa a média (reaproveita 100% o padrão de `Sheet` e `ContactDetailsPanel`).
- **Acessibilidade**: Alta (Radix Dialog gerencia foco, `Escape` para fechar e `aria-modal`).
- **Risco de regressão**: Mínimo (isolado fora da árvore do fluxo de mensagens).

### Opção 2: Aba Integrada ao Cabeçalho / Detalhes da Conversa

- **Descrição**: O `ChatView` divide seu corpo superior ou cabeçalho através de abas Radix (`Tabs`: "Mensagens", "Suporte & Equipamento", "Histórico"). Alternativamente, o suporte entra como uma aba adicional dentro do `ContactDetailsPanel`.
- **Desktop (>= 1024px)**: Se como aba no `ChatView`, oculta a thread de mensagens enquanto o operador preenche o chamado; se dentro de `ContactDetailsPanel`, divide espaço com "Mídia", "Notas" e "Histórico".
- **Tablet e Mobile**: Substitui a lista de mensagens pelo conteúdo da aba ativa.
- **Impacto no espaço das mensagens**: Alto (o atendente perde a visualização da conversa e das respostas do usuário enquanto preenche ou consulta dados do chamado).
- **Complexidade**: Média (exige alterar o ciclo de vida do scroll de mensagens ao alternar abas).
- **Acessibilidade**: Média (exige navegação por abas com setas e painéis rotulados).
- **Risco de regressão**: Médio (risco de desmontar elementos da timeline ou perder rascunho de mensagem digitada).

### Recomendação Fundamentada

> **Recomendação**: **Opção 1 (Drawer / Sheet lateral)**.
>
> **Justificativa**: O atendimento de suporte em TI exige que o técnico leia mensagens, copie detalhes relatados pelo usuário e confira o log enquanto preenche o chamado ou visualiza o status da máquina. A Opção 1 permite manter a conversa aberta em segundo plano (ou lado a lado no desktop), utiliza componentes já maduros no projeto (`@/components/ui/sheet`), não degrada o fluxo de mensagens WhatsApp e mantém a complexidade baixa e segura.

---

## C. Wireframes Textuais

### 1. Conversa sem painel aberto (cabeçalho da conversa)

```text
┌──────────────────────────────────────────────────────────────────────────┐
│ [←] (A) Maria Silva  [Conexão: Recepção] [Fila: TI]      [📞] [🔍] [🎧 Suporte] [Finalizar] │
├──────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  [14:20] Maria Silva: Boa tarde, a impressora de etiquetas travou...     │
│  [14:21] Técnico: Olá Maria, estou verificando a sua máquina agora.     │
│                                                                          │
├──────────────────────────────────────────────────────────────────────────┤
│ [📎] [😊] [Digite uma mensagem...                              ] [🎙️] [➤]│
└──────────────────────────────────────────────────────────────────────────┘
```

### 2. Painel de suporte aberto — Equipamento encontrado + Sem chamado ativo

```text
┌──────────────────────────────────────────────────┐
│ Suporte Técnico (GLPI / Tactical)            [X] │
├──────────────────────────────────────────────────┤
│ 🖥️ EQUIPAMENTO VINCULADO                         │
│ Hostname: SDE-ARS-RCP-02 [📋 Copiar]   [Trocar]  │
│ Secretaria: Saúde (SDE) · Unidade: Arsenio (ARS) │
│ Setor: Recepção (RCP-02) · Patrimônio: 049281    │
│ Match: [ Vinculado (matched) ]                   │
│                                                  │
│ ⚡ TACTICAL RMM                                  │
│ Status: 🟢 Online (Check-in há 2 min)            │
│ SO: Windows 11 Pro 23H2 · IP: 192.168.10.42      │
├──────────────────────────────────────────────────┤
│ 🎫 CHAMADO ATIVO                                 │
│ Nenhum chamado aberto para este atendimento.     │
│                                                  │
│ [ + Abrir Chamado no GLPI ]                      │
└──────────────────────────────────────────────────┘
```

### 3. Tactical Desabilitado (`features.tactical = false`)

```text
┌──────────────────────────────────────────────────┐
│ 🖥️ EQUIPAMENTO VINCULADO                         │
│ Hostname: SDE-ARS-RCP-02                [Trocar] │
│ Secretaria: Saúde · Unidade: Arsenio             │
│ Match: [ Local ]                                 │
│ (Seção Tactical não é renderizada)               │
├──────────────────────────────────────────────────┤
│ 🎫 CHAMADO ATIVO                                 │
│ [ + Abrir Chamado no GLPI ]                      │
└──────────────────────────────────────────────────┘
```

### 4. Tactical Temporariamente Indisponível (Warning `tactical_unavailable`)

```text
┌──────────────────────────────────────────────────┐
│ 🖥️ EQUIPAMENTO VINCULADO                         │
│ Hostname: SDE-ARS-RCP-02                [Trocar] │
│                                                  │
│ ⚡ TACTICAL RMM                                  │
│ ⚠️ Telemetria em tempo real temporariamente       │
│    indisponível. Abertura de chamado liberada.   │
├──────────────────────────────────────────────────┤
│ [ + Abrir Chamado no GLPI ]                      │
└──────────────────────────────────────────────────┘
```

### 5. Equipamento Não Encontrado / Sem Vínculo

```text
┌──────────────────────────────────────────────────┐
│ 🖥️ EQUIPAMENTO VINCULADO                         │
│ ℹ️ Nenhum computador vinculado a este contato.   │
│                                                  │
│ [ 🔍 Localizar e Vincular Computador ]           │
├──────────────────────────────────────────────────┤
│ 🎫 CHAMADO ATIVO                                 │
│ [ + Abrir Chamado Avulso ]                       │
└──────────────────────────────────────────────────┘
```

### 6. Troca de Equipamento (Device Picker modal/popover)

```text
┌──────────────────────────────────────────────────┐
│ Vincular Computador ao Atendimento           [X] │
├──────────────────────────────────────────────────┤
│ [ 🔍 Digite hostname, setor ou patrimônio...  ]  │
├──────────────────────────────────────────────────┤
│ Resultados encontrados (3):                      │
│ ○ SDE-ARS-RCP-01 (Recepção Guichê 1)             │
│ ● SDE-ARS-RCP-02 (Recepção Guichê 2 - Atual)     │
│ ○ SDE-ARS-PRE-01 (Pré-atendimento Triagem)       │
├──────────────────────────────────────────────────┤
│ [ Cancelar ]            [ Confirmar Vinculação ] │
└──────────────────────────────────────────────────┘
```

### 7. Formulário de Abertura de Chamado

```text
┌──────────────────────────────────────────────────┐
│ Novo Chamado GLPI                            [X] │
├──────────────────────────────────────────────────┤
│ Computador: SDE-ARS-RCP-02 (vinculado)           │
│                                                  │
│ Solicitante *                                    │
│ [ Maria Silva                                  ] │
│                                                  │
│ Título do Chamado * (1 a 200 caracteres)         │
│ [ Impressora de etiquetas travada              ] │
│                                                  │
│ Descrição detalhada * (até 8000 caracteres)      │
│ ┌──────────────────────────────────────────────┐ │
│ │ A impressora Zebra GK420t parou de responder │ │
│ │ após a troca da bobina de papel.             │ │
│ └──────────────────────────────────────────────┘ │
│                                                  │
│ Prioridade: [ 3 - Média ▾ ]                      │
│ Categoria GLPI (ID numérico opcional): [     ]   │
│ Localização GLPI (ID numérico opcional): [   ]   │
├──────────────────────────────────────────────────┤
│ [ Cancelar ]               [ Criar Chamado GLPI] │
└──────────────────────────────────────────────────┘
```

### 8. Chamado em Estado `processing`

```text
┌──────────────────────────────────────────────────┐
│ 🎫 CHAMADO EM ANDAMENTO                          │
│ [ ⏳ Sincronizando com GLPI... ]                 │
│                                                  │
│ Protocolo Local: req-9f82...                     │
│ Solicitante: Maria Silva                         │
│ Título: Impressora de etiquetas travada          │
│                                                  │
│ Enviando requisição segura ao GLPI. Aguarde...   │
│ (Botões de ação desabilitados para evitar duplic)│
└──────────────────────────────────────────────────┘
```

### 9. Chamado em Estado `synced` (Sucesso)

```text
┌──────────────────────────────────────────────────┐
│ 🎫 CHAMADO GLPI CRIADO COM SUCESSO               │
│ Status: 🟢 Sincronizado (synced)                 │
│                                                  │
│ Ticket GLPI: #14892                              │
│ [ 🔗 Abrir no GLPI ↗ ] [ 📋 Copiar Link ]        │
│                                                  │
│ Solicitante: Maria Silva                         │
│ Título: Impressora de etiquetas travada          │
│ Computador: SDE-ARS-RCP-02                       │
│ Criado em: 13/09/2026 14:32                      │
├──────────────────────────────────────────────────┤
│ [ 🔄 Trocar Equipamento Vinculado ]              │
└──────────────────────────────────────────────────┘
```

### 10. Chamado em Estado `retryable_error` (Falha Temporária)

```text
┌──────────────────────────────────────────────────┐
│ 🎫 CHAMADO COM FALHA TEMPORÁRIA                  │
│ Status: 🟡 Falha Recuperável (retryable_error)   │
│ Motivo: Falha de comunicação transitória GLPI    │
│                                                  │
│ A solicitação foi gravada com segurança local.   │
│ O ticket ainda não foi confirmado no GLPI.       │
│                                                  │
│ [ 🔄 Repetir Criação (Retry) ]                   │
│ [ 📝 Editar Equipamento ]                        │
└──────────────────────────────────────────────────┘
```

### 11. Chamado em Estado `unknown` (Ambiguidade de Timeout)

```text
┌──────────────────────────────────────────────────┐
│ 🎫 ATENÇÃO: RESULTADO AMBÍGUO                    │
│ Status: 🟠 Confirmação Pendente (unknown)        │
│ Motivo: Timeout/transporte durante envio ao GLPI │
│                                                  │
│ ⚠️ NÃO abra outro chamado manualmente!          │
│ A requisição pode ter sido recebida pelo GLPI.   │
│ Aguarde a verificação técnica.                   │
│                                                  │
│ Atendente: Informe o suporte interno/supervisor. │
│ Administrador: Utilize a ferramenta de conciliação│
│ abaixo após verificar o GLPI.                    │
├──────────────────────────────────────────────────┤
│ 🛡️ CONCILIAÇÃO ADMINISTRATIVA (Apenas Admin)      │
│ [ Reconciliar Ticket... ]                        │
└──────────────────────────────────────────────────┘
```

### 12. Reconciliação Administrativa (Modal Admin)

```text
┌──────────────────────────────────────────────────┐
│ Conciliação Administrativa de Chamado        [X] │
├──────────────────────────────────────────────────┤
│ Request ID: req-82a1...                          │
│ External ID esperado: wacalls-req-82a1...        │
│                                                  │
│ Escolha o desfecho verificado no GLPI:           │
│                                                  │
│ ○ Ticket foi criado no GLPI                      │
│   ID do Ticket GLPI: [ 14895 ]                   │
│   (O backend validará o ID e o external_id)      │
│                                                  │
│ ○ Ticket NÃO foi criado no GLPI                  │
│   (Transita para retryable_error com segurança)  │
├──────────────────────────────────────────────────┤
│ [ Cancelar ]              [ Executar Conciliação]│
└──────────────────────────────────────────────────┘
```

### 13. Chamado em Estado `failed` (Rejeição Terminal)

```text
┌──────────────────────────────────────────────────┐
│ 🎫 FALHA DEFINITIVA NA CRIAÇÃO                   │
│ Status: 🔴 Falha Permanente (failed)             │
│ Motivo: Dados rejeitados pelo GLPI (422/400)     │
│                                                  │
│ Este chamado não pode sofrer retry automático.   │
│ Corrija os dados e gere uma nova solicitação.    │
│                                                  │
│ [ + Tentar Nova Solicitação ]                    │
└──────────────────────────────────────────────────┘
```

### 14. Visualização Mobile (Tela Cheia)

```text
┌──────────────────────────────────────┐
│ [← Voltar] Suporte Técnico           │
├──────────────────────────────────────┤
│ 🖥️ SDE-ARS-RCP-02          [Trocar]  │
│ 🟢 Online · Win 11                   │
├──────────────────────────────────────┤
│ 🎫 Ticket GLPI: #14892               │
│ [ 🔗 Abrir no GLPI ↗ ]               │
│ Título: Impressora travada           │
│ Status: Sincronizado                 │
└──────────────────────────────────────┘
```

---

## D. Componentes Previstos

A arquitetura de componentes será organizada em `client/src/components/domain/support/`:

```text
client/src/components/domain/support/
├── SupportPanel.tsx             // Container principal do Sheet lateral
├── SupportPanelHeader.tsx       // Cabeçalho com status geral e botão fechar
├── DeviceSummary.tsx            // Bloco do computador (hostname, secretaria, setor, match)
├── TacticalStatus.tsx           // Status em tempo real do Tactical (online, SO, IP, warning)
├── DevicePickerModal.tsx        // Busca e seleção/troca de equipamento (Search bindings)
├── SupportRequestStatus.tsx     // Card do ticket com badge, link e informações do estado
├── SupportTicketForm.tsx        // Formulário de criação de chamado com validação
├── RetryAction.tsx              // Botão de retry com feedback de loading e concorrência
└── ReconcileDialog.tsx          // Modal restrito a administradores para desfecho de unknown
```

### Padrões e Convenções Adotadas:
- **Design System**: Ícones de `lucide-react`, botões de `@/components/ui/button`, badges de `@/components/ui/badge`, cards de `@/components/ui/card`, inputs de `@/components/ui/input`, textareas de `@/components/ui/textarea` e notificações via `sonner` (`toast.success`, `toast.error`, `toast.warning`).
- **Padrão de Diálogos**: Baseado em `@/components/ui/dialog` e `@/components/ui/sheet` (Radix Primitives).

---

## E. Serviços e Tipos TypeScript

### 1. Tipagem Oficial (`client/src/types/support.ts`)

```typescript
export type SupportSyncState =
  | "processing"
  | "synced"
  | "retryable_error"
  | "unknown"
  | "failed";

export type DeviceMatchStatus =
  | "pending"
  | "matched"
  | "conflict"
  | "missing_glpi"
  | "missing_tactical"
  | "disabled";

export interface SupportRequestDTO {
  id: string;
  sessionId: string;
  chatJid: string;
  title: string;
  description: string;
  requesterName: string;
  hostnameInformed: string;
  hostnameNormalized: string;
  deviceBindingId?: string;
  syncState: SupportSyncState;
  lastErrorCode?: string;
  attemptCount: number;
  glpiTicketId?: string;
  glpiTicketHref?: string;
  categoryId?: string;
  locationId?: string;
  priority: number;
  createdAt: number;
  updatedAt: number;
  processedAt?: number;
}

export interface DeviceBindingDTO {
  id: string;
  hostname: string;
  hostnameNormalized: string;
  glpiComputerId?: string;
  tacticalAgentId?: string;
  tacticalClientId?: string;
  tacticalSiteId?: string;
  sectorCode?: string;
  patrimonio?: string;
  matchStatus: DeviceMatchStatus;
  lastVerifiedAt: number;
  createdAt: number;
  updatedAt: number;
}

export interface TacticalAgentSummary {
  id: string;
  hostname: string;
  status: "online" | "offline" | "overdue";
  operatingSystem?: string;
  publicIp?: string;
  localIps?: string[];
  lastSeen?: string;
}

export interface ConversationSupportResponse {
  supportRequests: SupportRequestDTO[];
  currentSupportRequest?: SupportRequestDTO | null;
}

export interface SupportTicketResponseEnvelope {
  supportRequest: SupportRequestDTO;
  device?: DeviceBindingDTO | null;
  glpiComputer?: unknown | null;
  tacticalAgent?: TacticalAgentSummary | null;
  warnings: string[];
  glpiContextUpdated?: boolean;
}

export interface DeviceDetailResponse {
  device: DeviceBindingDTO;
  glpiComputer: unknown | null;
  tacticalAgent: TacticalAgentSummary | null;
  warnings: string[];
}

export interface SearchDevicesResponse {
  devices: DeviceBindingDTO[];
}

export interface CreateSupportTicketPayload {
  requesterName: string;
  title: string;
  description: string;
  deviceBindingId?: string;
  hostname?: string;
  categoryId?: string;
  locationId?: string;
  priority?: number;
}

export interface UpdateDevicePayload {
  deviceBindingId: string;
}

export interface ReconcilePayload {
  outcome: "synced" | "safe_to_retry" | "processing_orphaned";
  glpiTicketId?: string;
}

export interface SupportErrorDetail {
  code: string;
  message: string;
  retryAfterSeconds?: number;
}
```

### 2. Serviço de API (`client/src/services/support.ts`)

O serviço encapsula `fetch` através da infraestrutura de `apiUrl` do WACalls com tratamento tipado:

- `getChatSupportContext(sessionId, chatJid, signal?)`: `GET /api/sessions/{sid}/chats/{jid}/support`.
- `createChatSupportTicket(sessionId, chatJid, idempotencyKey, payload, signal?)`: `POST /api/sessions/{sid}/chats/{jid}/support/ticket` com header `Idempotency-Key`.
- `getSupportRequest(id, signal?)`: `GET /api/support/requests/{id}`.
- `retrySupportRequest(id, signal?)`: `POST /api/support/requests/{id}/retry`.
- `reconcileSupportRequest(id, payload, signal?)`: `POST /api/support/requests/{id}/reconcile`.
- `searchSupportDevices(query, limit?, signal?)`: `GET /api/support/devices?query=...&limit=...`.
- `getSupportDevice(id, signal?)`: `GET /api/support/devices/{id}`.
- `updateSupportRequestDevice(id, payload, signal?)`: `PUT /api/support/requests/{id}/device`.

---

## F. Máquina de Estados Visual

| Estado (`syncState`) | Badge / Cor / Ícone | Mensagem ao Usuário | Ações Permitidas | Ações Bloqueadas | Atendente vs Admin |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `processing` | ⏳ **Azul / Animação pulsante** (`bg-blue-500/15 text-blue-600`) | "Sincronizando com o GLPI..." | Visualizar dados digitados; fechar drawer | Novo envio; trocar equipamento; retry; reconcile | Igual para ambos |
| `synced` | 🟢 **Verde esmeralda** (`bg-emerald-500/15 text-emerald-600`, ícone `CheckCircle2`) | "Chamado aberto e sincronizado com sucesso" | Abrir link GLPI (`glpiTicketHref`); copiar ID; trocar equipamento vinculado | Retry; Reconcile | Igual para ambos |
| `retryable_error` | 🟡 **Âmbar** (`bg-amber-500/15 text-amber-600`, ícone `AlertTriangle`) | "Instabilidade temporária na integração" | Botão **Repetir Criação (Retry)**; trocar equipamento | Reconcile | Igual para ambos |
| `unknown` | 🟠 **Laranja forte** (`bg-orange-500/15 text-orange-600`, ícone `HelpCircle`) | "Confirmação pendente: não reenvie para evitar duplicidade" | Fechar drawer; verificar no portal GLPI | Retry comum; criação duplicada com mesma intenção | **Atendente**: orientação de aguardar.<br>**Admin**: botão **Reconciliar Ticket** |
| `failed` | 🔴 **Vermelho destrutivo** (`bg-red-500/15 text-red-600`, ícone `XCircle`) | "Solicitação rejeitada pelo sistema de tickets" | Abrir novo chamado (com nova intenção/chave) | Retry; Reconcile | Igual para ambos |

---

## G. Feature Flags e Tratamento de Degradação

### 1. Matriz de Feature Flags

- **`features.support = false`**:
  - O botão de atalho no cabeçalho do chat (`Support` icon) **não é renderizado**.
  - Nenhuma requisição às rotas `/api/support/*` é disparada.
- **`features.support = true` e `features.tactical = false` (Fallback Explícito)**:
  - O painel de suporte opera de forma completa e irrestrita: o operador pode abrir chamados no GLPI, consultar o status da sincronização, efetuar retry, reconciliar (admin) e trocar o equipamento vinculado.
  - A telemetria Tactical RMM é integralmente ocultada da interface (o componente de telemetria Tactical não é renderizado, e nenhum alerta ou warning de telemetria é gerado).
- **`features.support = true` e `features.tactical = true` com falha de conexão**:
  - A resposta da API inclui `warnings: ["tactical_unavailable"]`.
  - O painel exibe um alerta âmbar não impeditivo seguro: *"Não foi possível consultar a telemetria em tempo real do computador no momento. A criação e o vínculo do chamado prosseguem normalmente."*

### 2. Tratamento de Respostas HTTP

- **HTTP 503 (`support_disabled`)**: Interrompe o polling e exibe toast de aviso *"Módulo de suporte indisponível"*.
- **HTTP 429 (`rate_limited`)**: O backend envia header `Retry-After: 60`. O botão de envio/retry é desabilitado com contador regressivo em tela: *"Aguarde X segundos antes de tentar novamente"*.
- **HTTP 401 (`unauthorized`)**: Dispara redirecionamento para login (`useAuth.getState().refresh()`).
- **HTTP 403 (`forbidden`)**: Exibe mensagem *"Acesso negado a esta conversa ou operação administrativa"*.
- **HTTP 404 (`not_found`)**: Se for ao carregar chamado ou equipamento: *"Recurso não encontrado ou pertencente a outra empresa"*.
- **HTTP 409 (`idempotency_conflict`)**: Alerta *"Conflito de chave: esta solicitação já foi submetida com outros parâmetros. Recarregue a página."*
- **HTTP 409 (`state_conflict`)**: Alerta *"Operação em andamento ou estado do chamado alterado por outro atendente."* (Revalida o estado via GET).
- **HTTP 422 (`device_mismatch`)**: Alerta de formulário *"O hostname informado diverge do computador selecionado."*
- **HTTP 502 (`ticket_rejected` / `integration_auth_failed`)**: Exibe mensagem explicativa de falha de autenticação do backend com o GLPI.

---

## H. UX e Regras do Formulário de Abertura

### 1. Campos e Validações

- **Solicitante**:
  - Pré-preenchido com o nome do contato WhatsApp (`chat.name` ou peer formatado).
  - Obrigatório, entre 1 e 120 caracteres.
- **Título**:
  - Obrigatório, entre 1 e 200 caracteres.
- **Descrição**:
  - Obrigatório, entre 1 e 8.000 caracteres.
  - Campo expansível com contador de caracteres regressivo.
- **Equipamento / Hostname**:
  - Exibe o equipamento atualmente vinculado. Permite alternar via botão *"Trocar"* que abre o `DevicePickerModal`.
- **Prioridade**:
  - Seletor de 1 a 5 (Padrão: 3 - Média).
- **Categoria e Localização GLPI**:
  - Opcionais no MVP; aceitam apenas inteiros positivos em formato decimal texto (1 a 32 caracteres).

### 2. Ciclo de Vida da `Idempotency-Key`

Para garantir conformidade com a Seção 7 da T-005:
1. **Geração**: A chave é gerada no momento em que o formulário é montado para uma nova intenção de abertura, utilizando formato seguro:
   `const key = "ui-" + crypto.randomUUID();` (39 caracteres ASCII, dentro do intervalo exigido de 16 a 128 bytes).
2. **Estabilidade**: A chave é armazenada no estado local do formulário (`useState` ou ref) e **não muda** em re-renderizações acidentais ou perda temporária de conexão.
3. **Invalidação**: Se o usuário cancelar e reabrir o formulário para outro chamado, ou se um envio resultar em `409 idempotency_conflict`, uma nova chave é gerada.
4. **Proteção contra Duplo Clique**:
   - O botão de submissão entra em estado `loading` e fica `disabled` imediatamente no primeiro clique.
   - Qualquer tentativa adicional antes da resolução da Promise é descartada.
5. **Preservação de Rascunho**:
   - Em caso de falha transitória (erro 503, 429 ou erro de rede), o texto digitado pelo atendente é mantido no formulário para evitar retrabalho.
   - O formulário só é limpo após transição confirmada para `synced`, `retryable_error` ou `unknown`.

---

## I. Segurança e Acessibilidade (a11y)

1. **Proteção XSS e Renderização Segura**:
   - Toda renderização de texto (título, descrição, hostname, erros) usa strings puras do React (`{text}`). É **terminantemente proibido** o uso de `dangerouslySetInnerHTML`.
2. **Segredos e URLs**:
   - Nenhum token, credencial GLPI, API key Tactical ou URL interna de backend é exposta no bundle ou no state do cliente.
   - O link `glpiTicketHref` provém do backend já validado como safe link.
3. **Navegação por Teclado e Foco**:
   - O `Sheet` prende o foco dentro do painel enquanto aberto (`focus-trap` nativo do Radix).
   - Fechamento com tecla `Escape`.
   - Elementos interativos (botões de retry, trocar equipamento, links) possuem anéis de foco visíveis (`focus-visible:ring-2`).
4. **Leitores de Tela (`aria-*`)**:
   - Alertas de mudança de estado utilizam `aria-live="polite"`.
   - O estado do chamado possui `aria-atomic="true"` e rótulos claros para leitura assistiva.
5. **Contraste Visual**:
   - Badges e textos utilizam as classes do Tailwind configuradas no design system para modo claro e modo escuro, mantendo contraste mínimo WCAG 2.1 AA.
   - Não depender exclusivamente da cor: todo estado é acompanhado de ícone específico e texto explícito.

---

## J. Estratégia de Atualização de Estado

A T-005 não criou novos eventos de SSE para o domínio de suporte (conforme decisão de escopo da Seção 2 da T-005).

- **Revalidação Inicial**: Ao selecionar uma conversa no `ChatView`, se `features.support = true`, uma consulta `GET /api/sessions/{sid}/chats/{jid}/support` é disparada em segundo plano para obter o estado mais recente.
- **Durante Abertura de Chamado**: O `POST /ticket` responde de forma síncrona com o resultado inicial (201 `synced`, 202 `retryable_error`/`unknown` ou 502 `failed`).
- **Polling Limitado para `processing`**:
  - Se a resposta for `processing` (ou se o registro for encontrado em `processing` ao abrir a conversa), inicia-se um polling curto:
  - Intervalo: a cada 3 segundos, com teto máximo de 5 tentativas (máximo 15 segundos).
  - Assim que o estado mudar para qualquer estado terminal ou recuperável (`synced`, `retryable_error`, `unknown`, `failed`), o polling cessa imediatamente.
- **Após Retry / Reconcile**: Atualização imediata via resposta da própria requisição POST, sem polling contínuo.
- **Revalidação Manual**: Botão discreto de recarregar (`RefreshCw`) no cabeçalho do painel de suporte.

---

## K. Plano de Testes do Frontend

Quando a implementação da T-006 for autorizada, os seguintes testes serão obrigatórios:

1. **Feature Flags**:
   - Com `features.support = false`, o botão de suporte não aparece no chat.
   - Com `features.support = true` e `features.tactical = false`, a seção do Tactical é suprimida graciosamente.
2. **Ciclo dos 5 Estados**:
   - Renderização correta de `processing`, `synced`, `retryable_error`, `unknown` e `failed`.
3. **Formulário e Validações**:
   - Rejeição de campos em branco obrigatórios (título, descrição, solicitante).
   - Bloqueio de submissão enquanto `isSubmitting = true` (anti-duplo clique).
   - Preservação da `Idempotency-Key` durante o preenchimento.
4. **Erros HTTP e Retry-After**:
   - Tratamento correto de 429 com desativação temporária do botão de retry.
   - Tratamento de 503 com mensagem de serviço indisponível.
   - Tratamento de 409 com instrução de recarregar.
5. **Permissões de Reconcile**:
   - Botão de reconciliação visível exclusivamente quando `isAdmin(user) === true` em chamados com estado `unknown`.
6. **Troca de Equipamento**:
   - Busca no modal de seleção e atualização do binding vinculado ao chamado.
7. **Não-Regressão**:
   - Envio de mensagens de texto e mídia no WhatsApp continuando 100% funcional.
   - Troca de conversas e comportamento responsivo mobile preservados.

---

## L. Critérios de Aceite e Lista de Arquivos Previstos

### 1. Arquivos que serão criados na implementação futura

```text
client/src/types/support.ts
client/src/services/support.ts
client/src/components/domain/support/SupportPanel.tsx
client/src/components/domain/support/DeviceSummary.tsx
client/src/components/domain/support/TacticalStatus.tsx
client/src/components/domain/support/DevicePickerModal.tsx
client/src/components/domain/support/SupportTicketForm.tsx
client/src/components/domain/support/SupportRequestStatus.tsx
client/src/components/domain/support/RetryAction.tsx
client/src/components/domain/support/ReconcileDialog.tsx
```

### 2. Arquivos que serão tocados pontualmente na implementação futura

```text
client/src/components/domain/chat/ChatView.tsx  // Inclusão do botão de abrir o SupportPanel no header
client/src/services/settings.ts                // Tipagem das flags features.support e features.tactical em Options
```

### 3. O que NÃO será alterado na T-006

- Backend Go (`cmd/server/`, `internal/`): **Nenhuma alteração**.
- Banco de dados e DDL: **Nenhuma alteração**.
- Flow Builder (`FlowBuilderPage.tsx`, etc.): **Nenhuma alteração**.
- Canais de telefonia / VoIP: **Nenhuma alteração**.
- Dashboards gerais ou rotas de navegação do AppShell: **Nenhuma alteração**.

---

## M. Decisões Aprovadas pelo Usuário

1. **Layout Escolhido**:
   - **Opção 1 (Aprovada)**: Painel lateral deslizante (`Sheet` / Drawer à direita), utilizando o componente `@/components/ui/sheet` e seguindo o padrão de `ContactDetailsPanel`. Preserva 100% a timeline de mensagens do chat sem desmontagens.
2. **Fallback Tactical Desabilitado**:
   - Com `features.support=true` e `features.tactical=false`, o painel opera completamente para abertura e ciclo de vida do chamado GLPI, ocultando apenas a seção de telemetria Tactical.
3. **Proteção de Idempotência**:
   - Idempotency-Key estável mantida durante a submissão, com bloqueio imediato de submissão (anti-duplo clique).
4. **Resolução de Ambiguidades**:
   - Polling curto automático (3s, máx 5 tentativas) para chamados em estado `processing`.
   - Campos essenciais visíveis (Solicitante, Título, Descrição, Equipamento, Prioridade) e campos avançados opcionais (Categoria, Localização).
   - Mobile: Sheet ocupa tela cheia responsivamente (< 768px).
