# Padrão de hostnames

## Objetivo

O hostname identifica operacionalmente a secretaria, a unidade, o setor e a
posição do equipamento. É exibido no chamado e usado para busca e associação,
junto aos IDs permanentes do Tactical (`tactical_agent_id`) e do GLPI
(`glpi_computer_id`).

## Formato canônico

```text
{SECRETARIA}-{UNIDADE}-{SETOR}-{NN}
```

Exemplo:

```text
SDE-ARS-RCP-02
│   │   │   └── número sequencial
│   │   └────── setor/função
│   └────────── unidade
└────────────── secretaria
```

Regras:

- letras maiúsculas ASCII, números e hífen;
- sem espaços, acentos ou underscore;
- códigos estáveis e documentados;
- número com dois dígitos, inclusive quando há apenas um computador no setor;
- tamanho máximo operacional recomendado de 15 caracteres (compatibilidade
  Windows/NetBIOS legado);
- hostname único em toda a Prefeitura.

## Exemplos observados

```text
Site ESF_ARSENIO:
SDE-ARS-RCP-02  -> Recepção
SDE-ARS-PRE-01  -> Pré-atendimento
SDE-ARS-CLT-04  -> Consultório
SDE-ARS-COD-01  -> Odontologia
Outros:
SDE-BEA-ENF-01
SDE-BTH-CLT-03
```

Os significados completos dos códigos devem ser confirmados no inventário. Não
deduza nem crie códigos novos automaticamente.

## Nomes legados

Variações observadas fora do padrão:

```text
SDE-ARS-RCP01     (sem hífen antes do número)
SDE-BEA-COORD     (sem número)
SDE-BEA-FISIO
SDE-BEA-REUNI
```

Durante a transição:

- o ServiceOps aceita alias explícito para o hostname antigo;
- nunca faça matching aproximado silencioso para executar ação remota;
- normalize apenas caixa e espaços externos; não insira hífens supondo intenção;
- conflito ou ausência de correspondência exige confirmação técnica.

## Fonte e validação

```text
Client Tactical -> confirma secretaria
Site Tactical   -> confirma unidade
Hostname        -> identifica setor e equipamento
Agent ID        -> identidade permanente no Tactical
Computer ID     -> identidade permanente no GLPI
```

O prefixo do hostname e o Client/Site devem ser coerentes. Divergência gera
alerta de inventário, não correção automática.

## Uso na abertura do chamado

O mesmo hostname deve aparecer:

- no card do agente instalado no computador;
- em uma etiqueta física colada no gabinete;
- na pesquisa de equipamentos do ServiceOps;
- no Tactical;
- no ativo correspondente do GLPI.

Fluxos aceitos:

```text
Chamado no próprio computador -> agente envia o hostname automaticamente
Chamado para outro computador -> usuário informa o hostname do card/etiqueta
Chamado via WhatsApp          -> usuário informa o hostname do card/etiqueta
```

Resolução interna:

```text
hostname informado
        ↓ correspondência exata
device_binding
        ├── tactical_agent_id
        ├── glpi_computer_id
        ├── tactical_client_id
        ├── tactical_site_id
        └── sector_code
        ↓
ticket GLPI associado ao computador correto
```

Aceite entrada sem diferença entre maiúsculas/minúsculas e remova espaços
externos. Não faça correção aproximada automática. Nome inexistente, duplicado
ou ambíguo: mantenha em triagem manual e solicite confirmação.

## Renomeação controlada

Nomes fora do padrão são corrigidos manualmente, um equipamento por vez. **Não**
implementar renomeação automática ou em massa.

1. Registrar hostname antigo, novo, Agent ID Tactical e Computer ID GLPI.
2. Validar que o novo nome é único e respeita o padrão.
3. Executar a renomeação pelo Tactical sem reinício automático.
4. Reiniciar em janela adequada.
5. Confirmar que o mesmo Agent ID reapareceu com o novo hostname.
6. Confirmar que o GLPI atualizou o ativo sem criar duplicata.
7. Atualizar alias/histórico e encerrar a mudança.

Não faça renomeação em massa antes de um piloto completo.

## Registro de códigos

Preencher após o inventário (não inventar códigos):

| Tipo | Código | Nome exibido | Status |
|---|---|---|---|
| Secretaria | `SDE` | Secretaria da Saúde | Em uso |
| Unidade | `ARS` | ESF Arsênio (a confirmar) | Em uso |
| Unidade | `BEA` | A confirmar | Em uso |
| Unidade | `BTH` | A confirmar | Em uso |
| Setor | `RCP` | Recepção | Em uso |
| Setor | `PRE` | Pré-atendimento | A confirmar |
| Setor | `CLT` | Consultório | A confirmar |
| Setor | `COD` | Odontologia | A confirmar |
| Setor | `ENF` | Enfermagem | A confirmar |
