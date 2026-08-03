# 05 — Hooks `pre-commit` / `pre-push` e ativação no clone

**What to build:** os gates passam a rodar sozinhos no fluxo de git. Um commit dispara o
nível rápido e é abortado se formatação, lint, segredos ou testes unitários reprovarem. Um
push dispara o nível completo e é abortado se a suite de conformidade falhar ou se o daemon
Docker estiver parado. Num clone novo, um único comando de setup ativa os hooks versionados,
local ao repositório — sem copiar arquivo na mão e sem tocar em configuração global do git.
`--no-verify` continua existindo como válvula consciente de emergência.

Os hooks são adaptadores finos: cada um só chama o ponto de entrada no nível certo. Nenhum
comando de qualidade é redeclarado dentro deles.

**Blocked by:** 01 — Ponto de entrada `scripts/check` com o gate rápido base; 04 — Nível `full`: preflight de Docker e suite de conformidade

**Status:** ready-for-agent

- [ ] Hooks versionados no repositório, em diretório dedicado
- [ ] `pre-commit` apenas invoca o nível rápido; `pre-push` apenas invoca o nível completo
- [ ] Nenhuma lista de comandos de qualidade duplicada dentro dos hooks
- [ ] Comando único de setup aponta o git do clone para os hooks versionados
- [ ] Setup é local ao repo; `git config --global` não é alterado
- [ ] Commit com formatação quebrada aborta; commit com árvore limpa passa
- [ ] Push com Docker parado aborta com a mensagem sobre o daemon
- [ ] Push com suite de conformidade verde passa
- [ ] Gates rodam independentemente de quais paths estão staged
- [ ] Hooks são executáveis e funcionam a partir de qualquer subdiretório
