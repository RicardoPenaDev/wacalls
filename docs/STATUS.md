# Estado atual

Atualizado em: 2026-09-15
Fase atual: Fase 1 — MVP GLPI + Tactical (T-007 Gate 4 concluído e publicado;
Gate 5 interrompido); planejamento de realinhamento ServiceOps em andamento (T-008).
Tarefa atual: `T-008` — Realinhamento de produto (central técnica + portal do
card Windows). F0 (inventário técnico do card) concluído. Próximo passo técnico:
correção isolada da persistência automática de vínculo (D-025).

## Estado das Entregas e Fases

- **T-007 (MVP GLPI + Tactical):**
  - **Gates 1-3:** Aprovados.
  - **Gate 4 (GLPI + Tactical):** Concluído e publicado em `origin/main`
    (commits `a014e58`, `162e8cf` e `f883897`). CI verde.
  - **Gate 5 (Homologação de Interface):** Interrompido a pedido do usuário
    para realinhamento de produto (T-008).
  - **Painel lateral atual:** Baseline técnico provisório (não representa a
    experiência final ServiceOps).
  - **Defeito estrutural de persistência:** Identificado na resolução automática
    por hostname (`device_binding_id=NULL`, ver D-025) — pendente de correção.
  - **Homologação da interface final ServiceOps:** Nenhuma homologação concluída.

- **T-008 (Realinhamento de Produto e Card Windows):**
  - **F0 — Inventário técnico do card Windows:** Concluído. Código localizado
    e inspecionado em diretório externo ao repositório público.
    - App funcional em C# 12, .NET 8, WPF com `NotifyIcon` na bandeja do sistema.
    - Estado atual: coleta local de hostname, IPv4 e serial da BIOS via WMI;
      não utiliza `MachineGuid` nem `machineFingerprint()`.
    - Botão atual abre portal Self-Service do GLPI no navegador padrão.
    - Sem comunicação HTTP própria, sem portas abertas, sem autenticação de dispositivo.
    - Janela fixa de 360x470 sem redimensionamento; viável para evolução na mesma
      tecnologia mediante modularização em controles/telas (UserControls).
    - Código ainda não possui repositório Git próprio; deploy e atualizações dependem
      do Tactical RMM; `config.json` local adulterável; sem assinatura digital (Authenticode).
    - Estado futuro proposto: identificador de instalação estável baseado no Windows/`MachineGuid`
      mais credencial individual emitida pelo servidor com proteção nativa do Windows
      (escopo `CurrentUser` vs. `LocalMachine` a definir após validação de perfis).
      O identificador nunca equivale a autenticação.
  - **Fases e dependências da T-008:**
    - F0 — Inventário do card: Concluído.
    - Fix D-025 — Persistência do `device_binding_id`: Próxima.
    - F1 — Conversation Core: Depende de Fix D-025.
    - F2 — Filas e roteamento por unidade/setor: Depende do contexto de equipamento existente e F1.
    - F3 — Interface ServiceOps do técnico: Depende de F1 e F2.
    - F4 — Identidade do dispositivo e portal/chat no card: Depende de F0, Fix D-025, F1, F2 e F3.
    - F5 — Migração consentida Portal ↔ WhatsApp: Depende de F1 e F4.
    - F6 — Encerramento e sincronização GLPI: Depende de F1, F3 e F4.

## Matriz E2E (12/12, todos com assertion real no navegador)

1. Equipamento vinculado: hostname + badge "Vinculado".
2. `processing → synced`: refresh manual observa "Sincronizando" real.
3. Rascunho do composer + conversa preservados ao abrir/fechar o painel.
4. `support=true, tactical=false`: boot real dedicado sem Tactical.
5. Operador sem reconciliação em `unknown`.
6. Admin reconciliando `unknown`.
7. Retry após 429: bloqueado até prazo real (60s), liberado, sincronizado.
8. Duplo clique: exatamente 1 POST, 1 Idempotency-Key.
9. `webUrl` segura + target/rel + "Copiar número" com clipboard real.
10. Polling encerrado ao fechar o painel.
11. Polling encerrado ao trocar de conversa.
12. Network Guard: bloqueia HTTP/HTTPS/WebSocket externo, permite loopback.

## Decisões Arquiteturais

- **D-019:** Rate limiting — proposta em aberto para a Etapa 7.5.
- **D-020:** Hardening do link web do GLPI (Opção A restrita) — aceita.
- **D-021:** Modo E2E restrito (`-e2e-mode`), sessão sintética em memória,
  validação estrutural de `runDir`/DB temporário, CA privada restrita a
  loopback e validação única via `precheckE2EBoot`.
- **D-2B-01:** Ressalva do Gate 2B — mensagem manual adicional durante o teste
  controlado; não foi duplicidade do sistema.
- **D-022:** `parseLastSeen` do Tactical trata formato legado sem offset
  como não confiável em vez de assumir UTC/local.
- **D-023:** Observabilidade sanitizada de erros do Tactical: categorias tipadas
  e log sanitizado (hash de hostname, sem segredos).
- **D-024:** Parser tolerante para `local_ips` do Tactical: aceita string, lista
  separada por vírgula, array de strings e null sem quebrar processamento.
- **D-025:** Toda resolução automática exata de equipamento deve persistir o vínculo
  (`device_binding_id` em `support_requests`; `glpi_computer_id` conforme modelo atual;
  `tactical_agent_id` mantido em `device_bindings` sem duplicação). Correspondências
  ausentes ou ambíguas bloqueiam associação silenciosa.
- **D-026:** Requisitos de produto do portal do agente Windows: card compacto que expande
  para chat; lista de chamados ativos sem histórico pregresso completo (chamados encerrados
  não aparecem); abertura de chamado ativo não exibe histórico anterior mas permite novas
  mensagens e respostas subsequentes; nome e telefone opcional; computador atual (somente leitura)
  ou outro equipamento (digitação exata com escopo derivado da identidade do card e vínculo com
  a unidade, bloqueando sem match); descrição livre com título automático; ticket GLPI imediato;
  fila por unidade/setor; chat retomável; anexos em v1; notificações Windows e alerta no ícone;
  contingência offline; migração consentida para WhatsApp com envio inicial pelo WACalls;
  encerramento pelo técnico com aviso; e centralização técnica com WhatsApp em área secundária.

## Bloqueios e Próximos Passos Obrigatórios

1. **Próxima Ação Técnica:** Implementar a correção isolada da persistência de
   `device_binding_id` na criação automática de chamado (D-025) com testes unitários
   em ambiente isolado (sem chamadas externas e sem backfill em tickets passados).
2. Ticket **#4** no GLPI de homologação: registro formal do T-007 Gate 4, protegido e
   preservado. Ticket **#5**: demonstração visual complementar, intocado.
3. Confirmar timezone real do formato legado do Tactical caso volte a ocorrer (D-022).
4. **Instrução para a próxima IA:** Ler `AGENTS.md`, `docs/STATUS.md`, `docs/DECISIONS.md`
   e `docs/tasks/T-008-REALINHAMENTO-PORTAL-CARD.md`.

## Estado Git

- Branch: `main`.
- `origin/main` == `HEAD` em `f88389734a2e68b1a9fd8b274f74a3f18a97dee6`
  (7.4-R3 publicado, CI verde).
- Commits locais: nenhum commit criado nesta etapa.
- Push: N/A. Alterações restritas a documentação de alinhamento.
