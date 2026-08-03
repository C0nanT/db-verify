# 02 — Pre-commit versionado e ativação no clone

**What to build:** o fluxo de `git commit` passa a rodar o gate rápido
automaticamente. Os hooks vivem versionados no repositório (não só em
`.git/hooks` local). Há um setup de um comando que configura `core.hooksPath`
**somente no clone local** (nunca `--global`). Depois do setup, um commit com
código mal formatado, `go vet` vermelho ou testes unitários falhando é abortado;
um commit com a árvore saudável conclui normalmente. O hook só delega ao ponto de
entrada do ticket 01 — não duplica a lista de checagens.

**Blocked by:** 01 — Ponto de entrada do gate rápido

**Status:** ready-for-agent

- [ ] Hooks de pre-commit estão versionados no repo e apontam para o gate rápido
- [ ] Existe um comando/script de instalação que define `core.hooksPath` local ao
      repositório (não altera config global)
- [ ] Após a instalação, `git commit` com formatação quebrada aborta o commit
- [ ] Após a instalação, `git commit` com testes unitários falhando aborta o commit
- [ ] Após a instalação, `git commit` com árvore saudável (unitários verdes) conclui
- [ ] O gate roda sempre no pre-commit, independente de quais paths estão staged
- [ ] Não há formatação automática no stage; `--no-verify` do git continua disponível
      como válvula humana (não precisa ser documentado em profundidade aqui)
