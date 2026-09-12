================================================================================
           WACALLS CHAT - BACKUP COMPLETO DO PROJETO E DADOS
================================================================================
Data do Backup: 12/09/2026
Repositorio: https://github.com/RicardoPenaDev/wacalls

CONTEUDO DESTE BACKUP:
1. Codigo-fonte completo da aplicacao (frontend React/Vite + backend Go).
2. Dockerfile e docker-compose.yml prontos para deploy.
3. Pasta data_backup/:
   - wacalls.db: Banco SQLite com todas as conexoes e configuracoes salvas.
   - media/: Diretorio com arquivos de midia, avatares e chave de gravacao.
   - wacalls.db.backup_before_cleanup: Copia de seguranca anterior a limpeza.

COMO INICIAR ESTE BACKUP EM QUALQUER COMPUTADOR COM DOCKER:

1. Extraia o conteudo deste arquivo .zip em uma pasta de sua preferencia.
2. Abra o terminal (PowerShell, Prompt de Comando ou Terminal Linux) na pasta.
3. Para restaurar o banco de dados para o volume Docker:
   - Crie o volume e copie os dados:
     docker volume create wacalls_chat_data
     docker run --rm -v wacalls_chat_data:/data -v "${PWD}/data_backup:/backup" alpine sh -c "cp -a /backup/* /data/"
4. Inicie o sistema normalmente:
   docker compose up -d --build
5. Acesse no navegador:
   http://localhost:8090
   - Usuario admin: admin@admin.com
   - Senha padrao: admin
================================================================================
