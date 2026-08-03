# 01 — Ponto de entrada `scripts/check` com o gate rápido base

**What to build:** o contribuidor consegue rodar um único comando versionado que valida a
árvore inteira sem Docker. Invocado com o nível `fast`, ele executa, nesta ordem,
verificação de formatação, análise estática do Go e a suite unitária — parando na primeira
etapa que falhar e terminando com uma linha que nomeia a etapa culpada. Falha de formatação
lista os arquivos divergentes e o comando de correção. Funciona chamado de qualquer
subdiretório do repo, porque os hooks e o CI vão chamá-lo de contextos diferentes.

Este é o ponto de entrada único da spec: hooks e pipeline serão adaptadores finos por cima
dele, sem nunca duplicar a lista de comandos.

**Blocked by:** None — can start immediately.

**Status:** ready-for-human

- [x] `scripts/check fast` sai com 0 numa árvore saudável
- [x] Executa formatação → `go vet` → `go test ./...` (sem tag `docker`), nessa ordem
- [x] Para na primeira falha; não roda as etapas seguintes
- [x] Arquivo Go desformatado → exit ≠ 0, saída lista os arquivos e indica `gofmt -w`
- [x] Teste unitário quebrado → exit ≠ 0, mensagem final identifica a etapa como unit
- [x] Saída bruta de `go vet` e `go test` continua visível (não é engolida)
- [x] Mesmo resultado quando invocado de dentro de um subdiretório do repo
- [x] Nível desconhecido ou ausente → erro claro em pt-BR listando os níveis válidos
- [x] Mensagens ao humano em pt-BR; nomes de arquivo e flags em inglês
