# 02 — Ferramentas pinadas e `golangci-lint` no gate rápido

**What to build:** o repo passa a ter lint estático de verdade, com versão idêntica na
máquina de cada contribuidor e no CI. O contribuidor instala as ferramentas do gate com um
único comando, e a partir daí `scripts/check fast` reprova achados de staticcheck, errcheck,
ineffassign e unused — erros de retorno ignorados nas engines e código morto param de passar
batido. Quem rodar o gate sem ter instalado as ferramentas recebe uma mensagem dizendo qual
falta e como instalar, em vez de um erro críptico de comando não encontrado.

O pin é feito pelo próprio Go (tool dependencies no `go.mod`, invocadas via `go tool`), sem
introduzir toolchain fora do Go. Se isso se mostrar inviável para alguma ferramenta, o
fallback é execução direta do módulo numa versão fixa declarada em um único lugar do script
— nunca `@latest`.

Achados pré-existentes nas engines são corrigidos com o menor toque possível (tratar o erro
ou anotar uma exclusão justificada). Refatorar engine não faz parte deste ticket.

**Blocked by:** 01 — Ponto de entrada `scripts/check` com o gate rápido base

**Status:** ready-for-agent

- [ ] Um comando único instala/prepara todas as ferramentas do gate
- [ ] Versões pinadas e versionadas no repo; nenhuma resolução `@latest` em nenhum lugar
- [ ] `.golangci.yml` versionado habilitando ao menos staticcheck, errcheck, ineffassign, unused e govet
- [ ] Sem linters de estilo opinativo sobre nomes ou doc-comments (comentários do repo são pt-BR)
- [ ] `errcheck` relaxado em arquivos `_test.go`
- [ ] `scripts/check fast` roda o linter depois de `go vet` e antes dos testes
- [ ] Achado de linter → exit ≠ 0, mensagem final identifica a etapa como lint
- [ ] Ferramenta ausente → mensagem em pt-BR dizendo qual é e como instalar
- [ ] `scripts/check fast` verde na árvore atual do repo
- [ ] Nenhum arquivo de engine reestruturado além do mínimo para o linter passar
