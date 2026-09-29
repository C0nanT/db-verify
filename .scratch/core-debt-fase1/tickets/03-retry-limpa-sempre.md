# 03: Retry de porta remove o container em qualquer falha

> **Difficulty:** Standard: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** se a subida do container falha por qualquer motivo, e não só por conflito de porta (falha de pull da imagem, erro do daemon, ctx cancelado no meio do `docker run -d`), o helper de retry remove o container nomeado antes de devolver o erro. O operador não precisa rodar `docker rm` à mão. A remoção roda com um contexto próprio, desacoplado do cancelamento do chamador e com prazo curto, porque com o ctx já cancelado o `docker rm` não chegaria a executar. O doc de `Provision` no contrato passa a afirmar que um `Provision` com erro, inclusive por cancelamento, não deixa container nem outro recurso externo.

O fluxo de conflito (aviso, próxima porta, prompt ao esgotar, segundo ciclo) não muda.

Spec: `.scratch/core-debt-fase1/SPEC.md` (seções "Contrato" e "Helper de retry de porta"). Linha 1 do mapa `.scratch/tech-debt-map/core/2026-09-29.md`.

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] Um erro que não é conflito aborta o retry **e** registra `rm -f <nome>` (teste com `recRun`).
- [ ] Com o ctx cancelado antes ou durante a tentativa, a remoção também é registrada, e o ctx que o `Run` falso recebe na remoção não está cancelado.
- [ ] Os testes `TestStartWithPortRetry_*` existentes continuam passando; onde algum trava hoje a ausência de `rm` fora do conflito, a asserção é atualizada para o comportamento novo, com justificativa no commit.
- [ ] O doc de `Engine.Provision` descreve a garantia de limpeza em caso de erro ou cancelamento.
- [ ] `./scripts/check fast` passa.
