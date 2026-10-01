# 01: Remover `ExactCounts` de `ProvisionOpts`

> **Difficulty:** Light: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Suggestion only, use whatever model you have to hand.

**What to build:** a contagem exata de linhas chega às engines por um único caminho, o parâmetro `exact` de `Session.Collections`. O campo `ExactCounts` de `ProvisionOpts` não tem nenhum leitor e sai do contrato, junto com as atribuições que o preenchem: na montagem das opções em `run()` no `main`, nas opções padrão da suíte de conformidade da raiz e no teste de fluxo completo do Postgres (tag `docker`). A flag de contagem exata do CLI continua funcionando igual, porque o `main` segue repassando o valor para `Collections`.

Spec: `.scratch/contrato-engine-limpeza/SPEC.md`.

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] `ProvisionOpts` não tem mais o campo `ExactCounts`.
- [ ] Nenhuma atribuição a `ExactCounts` sobrevive em código de produção, nos testes sem tag ou nos testes com tag `docker`.
- [ ] O `main` continua passando a flag de contagem exata para `Session.Collections`, e a assinatura de `Collections` não muda.
- [ ] `./scripts/check fast` passa.
- [ ] `go vet -tags docker ./...` compila sem erro.
- [ ] Achado 7 registrado como resolvido (ou parcial, se o ticket 02 ainda estiver aberto) no "Andamento" de `.scratch/tech-debt-map/core/2026-09-29.md`.
