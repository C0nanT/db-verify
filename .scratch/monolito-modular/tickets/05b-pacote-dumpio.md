# 05b: Pacote `dumpio` e `HumanSize` no contrato

> **Difficulty:** Standard: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** três helpers ficaram na raiz ou no `detect` no ticket 05 e precisam de casa antes de qualquer engine migrar, porque o spec proíbe `engines/*` de importar `detect`:

- `OpenMaybeCompressed` (hoje em `internal/detect`, chamada por `detect` e por sqlite, redis, mongo, mysql e postgres) vai para um pacote novo `internal/dumpio`, que não importa nenhum outro pacote do projeto. `detect` e as engines passam a importá-lo.
- `humanSize` (hoje em `util.go`, usada por `main`, TUI, seletor e engines) vira `engine.HumanSize`, junto do tipo `Collection`, cujo campo `Size` ela formata.
- `printableStrings` (só o Postgres usa) **não** é movida agora: fica em `util.go` e passa a ser privada do pacote do Postgres no ticket 12, quando `util.go` deixa de existir.

Ajustar o `SPEC.md` na tabela "Pacotes" (linha nova para `dumpio`, sem imports do projeto) e nas "Fronteiras fiscalizadas" (`engines/*` e `detect` podem importar `dumpio`; `dumpio` não importa nada do projeto). Os apelidos temporários em `engine_aliases.go` que apontavam para `detect.OpenMaybeCompressed` passam a apontar para `dumpio`. Nenhum comportamento muda.

Spec: `.scratch/monolito-modular/SPEC.md` (seções "Pacotes" e "Fronteiras fiscalizadas").

**Blocked by:** 05

**Status:** ready-for-human

- [x] `internal/dumpio` existe, com `OpenMaybeCompressed`, e não importa nenhum pacote do projeto.
- [x] `internal/detect` importa `dumpio` e `engine`, e mais nada do projeto.
- [x] `engine.HumanSize` existe, com teste, e `humanSize` saiu de `util.go`.
- [x] `SPEC.md` descreve `dumpio` na tabela de pacotes e na regra do `depguard`.
- [x] `./scripts/check full` passa.
