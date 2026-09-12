# T-B002 — Harness MariaDB descartável para stores

Data: 2026-09-12
Estado: especificada; não implementada
Bloqueia: revisão final e implementação da T-005

## Objetivo

Criar somente infraestrutura de testes para executar uma mesma suíte de contrato
de stores contra SQLite e MariaDB. Não implementar `support_requests`, API de
suporte, integração GLPI/Tactical, Docker de produção ou mudança de runtime.

## Contrato do ambiente

- Docker/Compose sobe `mariadb:11.4`, linha LTS adotada como baseline esperada.
- Cada execução usa Compose project, container, volume, database e usuário
  exclusivos, todos com sufixo aleatório.
- Database lógico: prefixo `wacalls_store_test_`; usuário: prefixo
  `wacalls_test_`.
- Senhas aleatórias são geradas pelo script e existem somente nas variáveis
  `MARIADB_ROOT_PASSWORD`, `MARIADB_PASSWORD` e `WACALLS_TEST_MARIADB_DSN` do
  processo. Nenhum valor real ou default é salvo em arquivo, commit ou log.
- A porta `3306` não é publicada em todas as interfaces. Compose usa bind
  `127.0.0.1` com porta efêmera, descoberta por `docker compose port`.
- Storage é volume Docker temporário, nunca bind mount de diretório ou banco
  existente.
- Readiness usa o healthcheck oficial `healthcheck.sh --connect
  --innodb_initialized`, seguido de `SELECT 1` com o usuário exclusivo.
- Startup tem limite de 90 segundos; testes usam `go test -timeout=5m`; cleanup
  tem limite de 60 segundos.
- `trap`/`finally` sempre executa `docker compose down -v --remove-orphans`.
  Cleanup só aceita o project name aleatório criado pelo script e falha fechado
  se ele estiver vazio ou não tiver o prefixo `wacalls-store-contract-`.
- Nenhum GLPI, Tactical, WhatsApp ou serviço externo participa.

## Arquivos previstos na implementação da T-B002

```text
test/mariadb/compose.yml
internal/testdb/testdb.go
internal/testdb/testdb_test.go
scripts/test-store-contracts.ps1
scripts/test-store-contracts.sh
```

`internal/testdb` é helper exclusivo de testes; código de runtime não pode
importá-lo. A dependência de driver MariaDB é usada apenas pelo harness/testes.

## Interface da suíte de contrato

O helper abre:

- SQLite temporário por teste, com foreign keys, busy timeout e arquivo isolado;
- MariaDB pelo DSN efêmero fornecido pelo script.

A mesma função de contrato recebe `*sql.DB` e nome do backend. Ela deve permitir
mais de uma conexão/pool para testes concorrentes. Nenhum teste escolhe semântica
diferente por backend; somente abertura, DDL específico e cleanup variam.

## Smoke contract obrigatório

Executar nos dois backends:

1. criar database limpo e aplicar schema idempotente duas vezes;
2. abrir transação, inserir linha e confirmar commit visível;
3. validar unique constraint composta;
4. provocar inserção conflitante e reconhecer duplicate/unique violation;
5. executar CAS `UPDATE ... WHERE state=?` e afirmar `RowsAffected()==1` no
   vencedor e `0` no perdedor;
6. inserir dentro de transação, forçar erro e confirmar rollback integral;
7. abrir múltiplas conexões concorrentes e manter uma única vitória no CAS;
8. fechar pools, remover container/volume/rede pertencentes à execução;
9. propagar exit code não-zero de readiness, teste ou cleanup inválido.

Não criar tabela, modelo ou fixture `support_requests` nesta tarefa. Usar tabela
genérica mínima do próprio smoke contract.

## Comandos exatos

Windows/PowerShell:

```powershell
pwsh -NoProfile -File scripts/test-store-contracts.ps1 -Backend sqlite
pwsh -NoProfile -File scripts/test-store-contracts.ps1 -Backend mariadb
pwsh -NoProfile -File scripts/test-store-contracts.ps1 -Backend all
```

Linux/CI:

```bash
bash scripts/test-store-contracts.sh sqlite
bash scripts/test-store-contracts.sh mariadb
bash scripts/test-store-contracts.sh all
```

Comando Go invocado pelos scripts após preparar o backend:

```text
go test ./internal/testdb/... -run '^TestStoreHarnessContract$' -count=1 -timeout=5m
```

O modo `all` executa SQLite primeiro, MariaDB depois e sempre limpa o ambiente.
A execução CI usa os mesmos scripts e comandos, sem banco pré-provisionado.

## Critérios de aceite

- MariaDB 11.4 descartável fica healthy dentro do timeout.
- Database/usuário exclusivos funcionam sem privilégio sobre outros databases.
- Schema genérico sobe em banco limpo e novamente sem perda de dados.
- Transação, unique, conflito de insert, CAS/`RowsAffected` e rollback passam em
  SQLite e MariaDB.
- Múltiplas conexões disputam o CAS com um único vencedor.
- Reexecução local e CI não depende de estado anterior.
- Porta fica restrita a loopback e container/volume/rede da execução são
  removidos mesmo após falha.
- Credenciais/DSN não aparecem em arquivo, commit, log ou mensagem de teste.
- Exit code é zero somente quando readiness, contratos e cleanup válido passam.
- Nenhuma implementação T-005 ou dependência de GLPI/Tactical real é criada.

## Ordem após esta tarefa

```text
T-B002 — implementar e validar harness MariaDB
    ↓
revisão final do contrato T-005
    ↓
T-005 — implementar support_requests, eventos, API e wiring
```

T-005 permanece bloqueada até todos os critérios desta tarefa passarem.
