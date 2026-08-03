# 06 — Pipeline no GitHub Actions

**What to build:** toda mudança passa a ser validada fora da máquina de quem escreveu.
Um push para `main` ou a abertura de um pull request dispara o pipeline, que roda o **mesmo**
ponto de entrada dos hooks — sem um segundo conjunto de comandos para divergir. O relatório
do PR mostra dois jobs separados, para distinguir num relance falha de estilo/unit de falha
de integração: um job rápido e um job com a suite de conformidade em containers reais. O job
rápido acrescenta as checagens que só fazem sentido com rede e servidor: varredura de
vulnerabilidades nas dependências, verificação de que `go.mod`/`go.sum` estão limpos, e
compilação do binário (o pacote `main` não é coberto pelos testes). O mantenedor também
consegue disparar o pipeline manualmente, sem precisar de commit vazio.

**Blocked by:** 03 — Secrets scan com allowlist versionada; 04 — Nível `full`: preflight de Docker e suite de conformidade

**Status:** ready-for-agent

- [ ] Workflow dispara em push para `main`, em pull request e sob acionamento manual
- [ ] Jobs invocam o ponto de entrada versionado; nenhum comando de qualidade redeclarado no YAML
- [ ] Job rápido e job da suite Docker aparecem separados no relatório do PR
- [ ] Job rápido inclui varredura de vulnerabilidades das dependências
- [ ] Job rápido falha se `go mod tidy` produziria diff
- [ ] Job rápido compila o binário
- [ ] Job da suite roda `-tags docker` no runner, com os containers gerenciados pela própria suite
- [ ] Versão de Go derivada do `go.mod`, não hardcoded no workflow
- [ ] Cache de módulos e de build habilitado
- [ ] Timeout explícito no job pesado
- [ ] Defeito plantado em cada categoria (formatação, lint, secrets, unit, docker) deixa o job correspondente vermelho
- [ ] Árvore limpa deixa os dois jobs verdes
