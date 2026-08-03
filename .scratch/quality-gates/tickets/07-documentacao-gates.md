# 07 — Documentação dos gates de qualidade

**What to build:** contribuidor novo e agente encontram, sem adivinhar, o que cada gate roda,
quanto custa e como ativá-lo. O README ganha uma seção de qualidade explicando o ponto de
entrada e seus dois níveis, o que cada hook dispara, que o push exige daemon Docker, como
fazer o setup num clone novo e que `--no-verify` é válvula de emergência coberta pelo
pipeline. O `CLAUDE.md` passa a listar o ponto de entrada na seção de comandos, para agentes
usarem o mesmo caminho que o humano, e tem a regra "não há lint config separado" revogada —
o repo agora tem linter, secrets scan e configuração versionada dos dois.

**Blocked by:** 05 — Hooks `pre-commit` / `pre-push` e ativação no clone; 06 — Pipeline no GitHub Actions

**Status:** ready-for-agent

- [ ] README documenta os dois níveis do ponto de entrada e o que cada um roda
- [ ] README documenta o setup dos hooks num clone novo
- [ ] README deixa explícito que o gate de push exige daemon Docker
- [ ] README menciona `--no-verify` como válvula consciente, coberta pelo pipeline
- [ ] README descreve o que o pipeline valida em cada PR
- [ ] `CLAUDE.md` lista os comandos do ponto de entrada na seção Commands
- [ ] `CLAUDE.md` não afirma mais que o repo não tem lint config separado
- [ ] Documentação em pt-BR, alinhada à convenção do repo
- [ ] Nenhum fluxo alternativo de qualidade documentado fora do ponto de entrada único
