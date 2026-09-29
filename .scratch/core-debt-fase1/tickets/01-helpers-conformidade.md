# 01: Helpers da conformidade morando com a suíte

> **Difficulty:** Light: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** a suíte de conformidade genérica passa a compilar sem depender do teste de fluxo completo do Postgres. Os helpers que ela usa (checar se o Docker está disponível, checar se um container existe, gerar nome único, buscar coleção por nome) saem desse teste e vão para um arquivo de helpers ao lado da suíte, com a mesma build tag `docker`. É uma movimentação pura: nenhuma assinatura muda e nenhum comportamento de teste muda.

Spec: `.scratch/core-debt-fase1/SPEC.md` (seção "Helpers de teste da suíte de conformidade").

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] Os quatro helpers estão definidos fora do teste de fluxo do Postgres, num arquivo com build tag `docker` junto da suíte de conformidade.
- [ ] Removendo temporariamente o teste de fluxo do Postgres, `go vet -tags docker ./...` compila (verificação local; não commitar a remoção).
- [ ] `./scripts/check fast` e `./scripts/check full` passam sem alterar nenhuma asserção.
