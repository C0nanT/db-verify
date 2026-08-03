# 01 — Ponto de entrada do gate rápido

**What to build:** um ponto de entrada versionado no repositório que o contribuídor
(e, depois, os hooks) possa invocar à mão para validar a árvore antes de commit:
formatação (`gofmt`, só verificação — sem reescrever arquivos), `go vet` e
`go test ./...` (sem tag Docker). Em árvore saudável, exit 0; na primeira falha,
exit ≠ 0 com mensagem clara de qual etapa quebrou e, no caso de formatação,
quais arquivos divergem. Sem Node, sem linters além de `gofmt`/`go vet`.

**Blocked by:** None — can start immediately.

**Status:** ready-for-agent

- [ ] Existe um comando/alvo único e versionado que orquestra format → vet → unit
      nesta ordem e para na primeira falha
- [ ] Formatação usa verificação (`gofmt -l` ou equivalente); não altera arquivos
- [ ] Falha de formatação lista os arquivos fora do padrão e indica como corrigir
      (`gofmt -w`)
- [ ] `go vet ./...` e `go test ./...` (sem `-tags docker`) fazem parte do gate
- [ ] Saída dos comandos Go permanece visível no terminal (não engolida)
- [ ] Com a árvore atual saudável, o ponto de entrada termina com exit 0
- [ ] Nenhum hook git ainda é obrigatório neste ticket — só o ponto de entrada
      invocável fora do git
