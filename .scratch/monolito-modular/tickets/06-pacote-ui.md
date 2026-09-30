# 06: Pacote `ui` com o seletor de backups e a TUI

> **Difficulty:** Standard: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** o seletor interativo de backups e a TUI bubbletea passam a morar num pacote que conhece só o contrato (`engine`) e a detecção (`detect`). O `main` chama o seletor e abre a TUI com a `Session`, como hoje. O roteiro `run()` continua no `main`. Nenhum comportamento muda: mesmas telas, mesmas teclas, mesmos textos.

Spec: `.scratch/monolito-modular/SPEC.md` (seções "Pacotes" e "Orquestração").

**Blocked by:** 05b

**Status:** ready-for-human

- [x] O pacote `ui` importa só `engine` e `detect` dentre os pacotes do projeto.
- [x] Os testes do seletor e da TUI moram no pacote `ui`, sem asserção alterada.
- [x] `run()` continua no `main`.
- [x] `./scripts/check full` passa.
