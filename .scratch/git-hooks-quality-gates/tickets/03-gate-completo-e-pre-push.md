# 03 — Gate completo e pre-push

**What to build:** o fluxo de `git push` passa a exigir o gate completo: tudo do
gate rápido **mais** a suite `go test -tags docker ./...`. Antes da suite Docker,
o daemon precisa responder; se não estiver disponível, o push falha com mensagem
explícita pedindo Docker ligado — nunca pula em silêncio. O pre-push está
versionado ao lado do pre-commit, usa o mesmo mecanismo de `core.hooksPath` já
ativado no ticket 02, e só delega ao ponto de entrada estendido (rápido + docker),
sem duplicar comandos.

**Blocked by:** 01 — Ponto de entrada do gate rápido; 02 — Pre-commit versionado e ativação no clone

**Status:** ready-for-agent

- [ ] O ponto de entrada oferece um modo/alvo "completo" = gate rápido + checagem
      Docker + `go test -tags docker ./...`
- [ ] Se `docker info` (ou equivalente) falhar, o gate completo exit ≠ 0 com
      mensagem pedindo o daemon — sem pular a suite
- [ ] Hook `pre-push` versionado invoca o gate completo (não só a suite Docker)
- [ ] Com hooks ativados e Docker parado, `git push` é abortado com mensagem clara
- [ ] Com hooks ativados, Docker ok e suite verde, o caminho de push não é bloqueado
      pelo gate (smoke: invocar o gate completo com exit 0 basta se push real for
      inconveniente)
- [ ] Pre-push não adiciona lógica extra de skip por tipo de ref além do comportamento
      padrão do git
