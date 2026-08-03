# 04 — Nível `full`: preflight de Docker e suite de conformidade

**What to build:** o contribuidor ganha um segundo nível do mesmo ponto de entrada, que
valida a integração real das engines. `scripts/check full` roda tudo do nível rápido e, em
seguida, a suite de conformidade com tag `docker` — a mesma que define "a engine está pronta"
no `CLAUDE.md`. Antes de tentar subir qualquer container, o script verifica se o daemon Docker
responde; se não responder, falha na hora com uma mensagem explícita pedindo o daemon, em vez
de deixar a suite estourar com erro obscuro ou — pior — passar batido.

**Blocked by:** 01 — Ponto de entrada `scripts/check` com o gate rápido base

**Status:** ready-for-agent

- [ ] `scripts/check full` executa todas as etapas do nível `fast` antes das de integração
- [ ] Preflight do daemon Docker roda **antes** da suite pesada
- [ ] Daemon parado → exit ≠ 0 com mensagem em pt-BR pedindo o Docker; suite não é iniciada
- [ ] Nunca pula a suite em silêncio quando o Docker está indisponível
- [ ] Com Docker ok, roda `go test -tags docker ./...`
- [ ] Suite de conformidade vermelha → exit ≠ 0, etapa identificada como docker
- [ ] Árvore saudável com Docker ligado → exit 0
- [ ] Falha no nível rápido aborta antes de qualquer container subir
