# 07 — Documentação dos gates de qualidade

**What to build:** contribuidor novo e agente encontram, sem adivinhar, o que cada gate roda,
quanto custa e como ativá-lo. O README ganha uma seção de qualidade explicando o ponto de
entrada e seus dois níveis, o que cada hook dispara, que o push exige daemon Docker, como
fazer o setup num clone novo e que `--no-verify` é válvula consciente de emergência (não
há pipeline cobrindo o bypass). O `CLAUDE.md` passa a listar o ponto de entrada na seção
de comandos, para agentes usarem o mesmo caminho que o humano.

**Blocked by:** 05 — Hooks `pre-commit` / `pre-push` e ativação no clone

**Status:** ready-for-human

- [x] README documenta os dois níveis do ponto de entrada e o que cada um roda
- [x] README documenta o setup dos hooks num clone novo
- [x] README deixa explícito que o gate de push exige daemon Docker
- [x] README menciona `--no-verify` como válvula consciente, sem cobertura de pipeline
- [x] `CLAUDE.md` lista os comandos do ponto de entrada na seção Commands
- [x] `CLAUDE.md` não afirma mais que o repo não tem lint config separado
- [x] Documentação em pt-BR, alinhada à convenção do repo
- [x] Nenhum fluxo alternativo de qualidade documentado fora do ponto de entrada único
