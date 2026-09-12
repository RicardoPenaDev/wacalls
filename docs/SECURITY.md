# Segurança mínima

## Segredos

- Tokens do GLPI, Tactical, WhatsApp e banco ficam somente em secret manager ou
  variáveis de ambiente protegidas.
- Forneça `.env.example` apenas com nomes e valores fictícios.
- Nunca exponha token em URL, screenshot, log ou frontend.
- Faça rotação imediata se um segredo entrar no Git.

## Portal do agente

- Não confie em hostname, IP ou patrimônio enviados livremente pelo navegador.
- Use contexto assinado, de curta duração e com nonce para impedir replay.
- O token identifica o dispositivo; não concede acesso às APIs internas.
- "Outro equipamento" deve ser selecionado em uma lista autorizada da unidade.

## Permissões

- Visualizar ticket não implica executar ação remota.
- Scripts, reboot, terminal e remoto exigem perfis explícitos.
- Ações sensíveis precisam de confirmação e auditoria.
- Contas de integração usam o menor privilégio possível.
- Funcionários comuns pesquisam equipamentos somente na sua secretaria/unidade;
  técnicos autorizados podem ter visão municipal.
- Client e Site do Tactical participam do controle de escopo, mas não substituem
  a autorização do usuário.
- O isolamento por tenant (empresa) já existente no código continua valendo; não
  o confunda com escopo por secretaria (ver `docs/DECISIONS.md`, D-009).

## Rede

- GLPI, Tactical e bancos não devem ser expostos diretamente por conveniência.
- Backend faz chamadas server-to-server.
- Defina allowlists e TLS onde suportado.
- Separe desenvolvimento/homologação de produção.

## Dados pessoais

- Colete apenas dados necessários para suporte.
- Evite armazenar conteúdo integral de conversa fora dos sistemas definidos.
- Masque telefone, tokens e conteúdo sensível nos logs.
- Defina retenção e acesso ao histórico conforme regras da Prefeitura/LGPD.

## Operação segura

- Testes não enviam WhatsApp real nem executam ações Tactical reais por padrão.
- Integrações devem oferecer modo mock/sandbox.
- Toda feature de ação remota deve ter kill switch (feature flag).
- Migrações precisam de backup, plano de rollback e janela aprovada. Migrações de
  identidade de conversa (Fase 4A) devem ser aditivas e reversíveis.
