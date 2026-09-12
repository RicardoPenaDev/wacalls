# T-B001 — Recuperar baseline: pacote ausente internal/voip/media

Tipo: **tarefa de bloqueio** (bloqueia `T-003` e qualquer build/test de
`cmd/server`). Investigação **somente leitura** concluída em 2026-09-11.
Nada foi restaurado, gerado ou alterado. Sem stubs, sem mexer nos imports.

## Problema

`go build ./...` e `go test ./...` falham porque `internal/voip/call` importa
`wacalls/internal/voip/media`, mas o diretório `internal/voip/media/` não existe
nesta cópia. Por transição (`cmd/server/session.go`, `cmd/server/callregistry.go`
→ `internal/voip/call`), o pacote `cmd/server` não compila.

## Causa-raiz confirmada

`.gitignore` contém, na linha 20, o padrão **`media/`** (sem barra inicial),
pensado para a pasta de uploads em runtime. Sem âncora, esse padrão casa
**qualquer** diretório chamado `media` em qualquer profundidade — inclusive o
pacote-fonte `internal/voip/media/`. Evidência direta:

```text
$ git check-ignore -v internal/voip/media/pipeline.go
.gitignore:20:media/    internal/voip/media/pipeline.go
```

Resultado: o pacote-fonte nunca foi rastreado nem enviado ao GitHub. O fluxo de
instalação contorna isso distribuindo a pasta dentro do artefato `wacalls.zip`
(ver `instalador.sh:880-890` e `client/scripts/instalador_wacalls.sh:866-876`,
que re-extraem `internal/voip/media` de um `wacalls.zip` quando ausente).

## Evidências (verificações somente leitura)

1. **Git status/branch/commit**: `## main...origin/main`; remote
   `origin https://github.com/RicardoPenaDev/wacalls.git`; branch única `main`;
   HEAD `ec04bd2a0198ba10c3c6c5ec5cc694d1b5e43a57`
   ("fix(layout): fixa barra de digitacao..."). Sem tags. 7 commits no total.
   (Untracked locais: `AGENTS.md`, `CLAUDE.md`; `docs/` é ignorado por
   `.gitignore:1:/docs`.)
2. **Rastreados em `internal/voip`**: subpastas `call/`, `core/`, `signaling/`,
   `transport/`, `wanode/`. **Nenhum** `internal/voip/media/*` rastreado
   (HEAD e `origin/main`).
3. **.gitignore / .gitattributes / LFS / submodules**: `.gitignore` ignora
   `media/` (causa) e `/docs`. **Não há** `.gitattributes`, **não há**
   `.gitmodules`, `git lfs ls-files` vazio. Logo: sem submodule, sem LFS.
4. **Busca em branches/tags/histórico**: `git log --all -- 'internal/voip/media/**'`
   → vazio. O diretório **nunca** existiu em nenhum commit.
5. **Geração (go generate / build)**: `git grep "go:generate"` → nenhum. Os
   scripts de instalação **não geram** o pacote; eles apenas **re-extraem** de
   `wacalls.zip`. Portanto **não é gerado**, é código-fonte distribuído fora do Git.
6. **Nome/capitalização**: nenhuma variante (`Media`, `MEDIA`, etc.) em qualquer
   path do histórico. Não é caso de renomeação/case.
7. **Remote/outro branch**: `origin/main` também não contém `internal/voip/media`
   (só `callmanager_media.go`, que é do pacote `call`). Não há outro branch.
8. **Cópia no diretório-pai**: busca em `D:/fabrica/WaCalls` e `D:/` não achou
   nenhum diretório `internal/voip/media` nem um `wacalls.zip` de distribuição
   (apenas `WACalls_AI_Kit.zip`, que é o kit de docs, sem relação).
9. **Primeiro commit a importar**: `79321a1`
   ("feat: WaCalls Chat com suporte a botoes de link no fluxo e dockerizacao
   completa") — introduziu `wacalls/internal/voip/media`; o import não mudou desde
   então. Já nasceu importando um pacote que o Git ignorava.
10. **Onde está o pacote?**
    - outro branch/commit: **não**;
    - submodule/LFS: **não**;
    - gerado no build: **não** (re-extraído de `wacalls.zip`);
    - outra cópia local: **não encontrada** neste ambiente;
    - ausente de todo o histórico: **sim** (ignorado por `.gitignore media/`).

## Superfície do pacote (para a futura recuperação — não implementar)

`internal/voip/call/*.go` referencia estes símbolos exportados de `media`:

```text
media.Codec              media.DefaultCodecOptions   media.NewMLowCodec
media.RtpSession         media.NewWhatsAppOpusSession
media.SrtpSession        media.NewSrtpSession        media.DerivePerJidSrtpKey
media.NewH264Session     media.GenerateCallKey       media.GenerateSecureSsrc
```

Ou seja, um pacote real de mídia VoIP (RTP/SRTP/Opus/H264/codec/keying). **Não
reconstruir por dedução** — recuperar a fonte legítima.

## Fonte legítima identificada (não restaurar ainda)

- **Primária**: o artefato de distribuição `wacalls.zip` referenciado pelos
  instaladores (esperado em `/root` no servidor), que contém
  `internal/voip/media/`.
- **Equivalentes**: a árvore de trabalho do mantenedor (`RicardoPenaDev`) ou o
  servidor de produção onde o instalador já extraiu `${app_dir}/internal/voip/media`.
- Caminho/branch/commit no Git: **inexistente** (nunca versionado).
- Disponível neste ambiente: **não** (nenhum `wacalls.zip` nem cópia local).

## Opções de recuperação (decisão do mantenedor)

1. **Obter `wacalls.zip` atualizado** (ou a pasta `internal/voip/media/` do
   servidor/máquina do autor) e copiá-la para `internal/voip/media/`. Fonte
   legítima segundo o próprio instalador.
2. **Corrigir a causa-raiz no `.gitignore`**: trocar `media/` por `/media/`
   (ancorado à raiz, junto de `/media/` já existente) para o padrão deixar de
   engolir `internal/voip/media/`; então **versionar** a pasta recuperada. Fix
   durável — depende de obter a fonte primeiro (passo 1).
3. **Pedir ao mantenedor** o commit/push do pacote após corrigir o `.gitignore`.
4. **Não recomendado / proibido agora**: reconstruir o pacote a partir dos
   símbolos. É reimplementação de mídia VoIP, fora do escopo e arriscado.

## Critério para desbloquear T-003

- `internal/voip/media/` presente (via opção 1) **e** `.gitignore` ajustado
  (opção 2) para não voltar a ignorá-lo;
- `go build ./...` e `go test ./...` deixam de falhar por pacote ausente
  (baseline em `docs/STATUS.md` volta a compilar `cmd/server`).

## Fora do escopo desta tarefa

- Restaurar, gerar ou stubar `internal/voip/media`.
- Alterar imports em `internal/voip/call`.
- Editar `.gitignore` (apenas recomendado; aplicar na tarefa de recuperação).
- Qualquer implementação da T-003.

## Resultado obtido

Investigação concluída; causa-raiz e fonte legítima identificadas. Aguardando
decisão/insumo do mantenedor (artefato `wacalls.zip` ou pasta do servidor).
T-003 permanece **bloqueada**.
