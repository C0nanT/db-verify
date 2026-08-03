# 04 — Documentação dos gates no README e CLAUDE.md

**What to build:** um contribuídor (ou agente) que abre o repo entende como ativar
os hooks e o que cada um roda, sem caçar scripts. O README ganha uma seção de
qualidade local / hooks (ativação de um comando, pre-commit vs pre-push, necessidade
de Docker no push). O `CLAUDE.md` lista o ponto de entrada nas Commands, para
agentes usarem o mesmo caminho que os hooks. A documentação reflete o comportamento
já entregue nos tickets 01–03 — sem prometer CI remota, linters extras ou auto-format
no stage.

**Blocked by:** 02 — Pre-commit versionado e ativação no clone; 03 — Gate completo e pre-push

**Status:** ready-for-agent

- [ ] README explica como ativar os hooks no clone (comando de setup / `core.hooksPath`)
- [ ] README descreve o que roda no pre-commit vs no pre-push
- [ ] README deixa explícito que o pre-push exige Docker disponível e falha se o
      daemon estiver down
- [ ] `CLAUDE.md` documenta o ponto de entrada (gate rápido e completo) na seção de
      Commands
- [ ] Nada na documentação sugere Husky/npm/Prettier ou `golangci-lint` como parte
      deste fluxo
