# 13: Remover apelidos da raiz e ligar o `depguard`

> **Difficulty:** Standard: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** o passo de **contração**. Com todo o código fora da raiz, os apelidos e repasses temporários criados nos tickets 01, 03 e 04 são removidos, e o `main` passa a referenciar os pacotes diretamente. Em seguida, a direção das dependências vira regra que reprova o commit: uma configuração `depguard` no lint, rodando pelo `./scripts/check fast` como o resto do lint:

- nenhuma engine importa outra engine, `ui`, `detect` nem `main`;
- `ui` e `detect` não importam nenhuma engine concreta;
- `engine` não importa nenhum outro pacote do projeto.

Spec: `.scratch/monolito-modular/SPEC.md` (seção "Fronteiras fiscalizadas").

**Blocked by:** 05, 05b, 06, 08, 09, 10, 11, 12

**Status:** ready-for-agent

- [ ] A raiz tem só o `main` (flags, `run()`, lista explícita de engines) e os testes do conjunto montado; nenhum apelido ou repasse temporário sobra.
- [ ] As regras acima estão no `.golangci.yml`, com comentário em pt-BR.
- [ ] Uma importação proibida feita de propósito (por exemplo uma engine importando outra) faz `./scripts/check fast` reprovar. Mudança revertida depois.
- [ ] `scripts/check` e os hooks não foram alterados.
- [ ] `./scripts/check full` passa.
