# 02: Comentário de `Match.Confidence` aponta para as constantes

> **Difficulty:** Light: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Suggestion only, use whatever model you have to hand.

**What to build:** quem lê o contrato de `Match` encontra no campo `Confidence` uma referência às constantes `ConfidenceMagic`, `ConfidenceExtension` e `ConfidenceGuess`, e não uma cópia dos valores `100/50/10`. Assim o comentário não fica desatualizado quando uma constante mudar. É mudança só de comentário, em pt-BR, sem efeito em comportamento.

Spec: `.scratch/contrato-engine-limpeza/SPEC.md`.

**Blocked by:** None (can start immediately)

**Status:** ready-for-human

- [x] O comentário de `Match.Confidence` não repete nenhum valor numérico de confiança e cita as constantes `Confidence*`.
- [x] Os valores das constantes e a regra de desempate da detecção continuam iguais.
- [x] `./scripts/check fast` passa.
- [x] Achado 7 registrado como resolvido (ou parcial, se o ticket 01 ainda estiver aberto) no "Andamento" de `.scratch/tech-debt-map/core/2026-09-29.md`.
