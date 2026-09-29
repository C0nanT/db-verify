# 03: Pacote `relational` com a heurística de coluna de ordenação

> **Difficulty:** Light: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** a heurística de "qual coluna ordena os mais recentes" (camadas de nomes conhecidos, escolha da coluna, hint da TUI e helpers de dialeto compartilhados) passa a morar num pacote próprio, usado só pelas engines relacionais (Postgres, MySQL/MariaDB, SQLite). Enquanto essas engines estão na raiz, ela as alcança por apelidos e repasses, como no ticket 01. Nenhum comportamento muda.

Spec: `.scratch/monolito-modular/SPEC.md` (seção "Pacotes").

**Blocked by:** 01

**Status:** ready-for-human

- [x] O pacote `relational` importa no máximo `engine` dentre os pacotes do projeto.
- [x] Os testes da heurística moram no pacote `relational`, sem asserção alterada.
- [x] Os repasses na raiz ficam junto dos apelidos do ticket 01.
- [x] `./scripts/check full` passa.
