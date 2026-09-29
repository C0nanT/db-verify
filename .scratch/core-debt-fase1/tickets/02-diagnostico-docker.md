# 02: Diagnóstico e prazo na checagem do Docker

> **Difficulty:** Standard: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** quando o Docker não está disponível, o operador vê o motivo real, isto é, a saída do `docker info` (permissão no socket, daemon parado, `DOCKER_HOST` errado), e não só "docker daemon não está acessível". Se o daemon estiver travado, a checagem desiste depois de um prazo fixo (~10 s) em vez de bloquear para sempre, e respeita o cancelamento do ctx do `Provision`. As mensagens atuais continuam como prefixo.

Spec: `.scratch/core-debt-fase1/SPEC.md` (seção "Checagem do Docker"). Linha 3 do mapa `.scratch/tech-debt-map/core/2026-09-29.md`.

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] O erro de `Available` contém a saída do `docker info` (sem espaços nas pontas) e encadeia o erro original (recuperável com `errors.Is`/`errors.As`); o erro de "não encontrado no PATH" também encadeia o original.
- [ ] O wrapper usado pelas engines recebe o ctx do `Provision` e aplica um prazo fixo sobre ele; as 5 engines Docker passam o ctx.
- [ ] Teste unitário com `Run` falso (`recRun`) que devolve saída e erro: a mensagem contém a saída e o erro original.
- [ ] Teste unitário com `Run` bloqueante e ctx com prazo curto: `Available` retorna logo, com erro de prazo.
- [ ] O teste existente de `Available` continua passando sem mudar as substrings verificadas.
- [ ] `./scripts/check fast` passa.
