# 06: Desempate por posição ordinal no Postgres

> **Difficulty:** Standard: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** quando uma tabela Postgres tem duas colunas na mesma camada da heurística (por exemplo `created_at` e `inserted_at`), "os mais recentes" são sempre ordenados pela coluna de menor posição ordinal, igual ao MySQL, MariaDB e SQLite. Hoje o `DISTINCT ON` do Postgres não tem desempate, e a coluna escolhida pode mudar entre execuções sobre o mesmo backup. É a única mudança de comportamento da fase (decisão 1 do mapa). Tabelas sem empate continuam com a mesma coluna e o mesmo hint na TUI.

Spec: `.scratch/core-debt-fase1/SPEC.md` (seção "Heurística de coluna de ordenação", item Postgres). Linha 2 do mapa.

**Blocked by:** 05 (heurística de ordenação inteira no módulo relacional)

**Status:** ready-for-human

- [x] A consulta de tabelas do Postgres usa a posição ordinal da coluna como critério depois da preferência.
- [x] O teste de fluxo completo do Postgres (tabela de camadas) ganha uma tabela com `created_at` e `inserted_at`, em que `created_at` tem posição ordinal menor: a ordenação escolhida é `created_at` e o hint indica data.
- [x] Os demais casos da tabela de camadas continuam passando sem alteração.
- [x] `./scripts/check full` passa.
