# 02: Lista explícita de engines na raiz, no lugar dos `init()`

> **Difficulty:** Standard: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** as engines deixam de se registrar sozinhas. Os seis `init()` que chamam `Register` somem, e um único lugar no pacote `main` lista as engines, na **mesma ordem em que hoje acabam registradas** (a ordem alfabética dos arquivos: mariadb, mongo, mysql, postgres, redis, sqlite), para que o desempate na detecção não mude. Quem abrir o `main` vê num lugar só quais engines existem.

Armadilha: os testes da raiz (detecção sobre backups reais, suíte de conformidade, fluxo do Postgres) dependem do registro estar preenchido, e `main()` não roda nos testes. A lista precisa rodar antes de qualquer uso do registro **tanto no binário quanto nos testes**.

Spec: `.scratch/monolito-modular/SPEC.md` (seção "Registro das engines").

**Blocked by:** 01

**Status:** ready-for-human

- [x] Nenhum arquivo de engine chama `Register` em `init()`.
- [x] Existe uma única lista de engines no pacote `main`, com comentário em pt-BR explicando que a ordem decide o desempate de detecção.
- [x] `--list-engines` mostra as mesmas engines, na mesma ordem de antes.
- [x] Os testes de detecção sobre `testdata/headers/` e a suíte de conformidade continuam vendo todas as engines.
- [x] `./scripts/check full` passa.
