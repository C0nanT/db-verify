# Quality gates: pre-commit, pre-push e pipeline no GitHub

Status: ready-for-agent

## Problem Statement

O `db-verify` já tem seis engines (Postgres, MySQL, MariaDB, SQLite, Redis, MongoDB)
atrás da mesma costura `Engine`/`Session`, com duas camadas de teste — a unitária
(`go test ./...`, sem Docker) e a de conformidade/integração (`go test -tags docker
./...`, containers reais por engine). Nada disso é executado por obrigação: `core.hooksPath`
já aponta para `.githooks/`, mas o diretório está vazio, e não existe `.github/workflows/`.

Na prática:

- código pode ser commitado sem `gofmt`, com `go vet` sujo ou com a suite unitária vermelha;
- uma engine nova pode ser empurrada sem nunca ter passado na suite genérica de
  conformidade — que é justamente o critério de "a engine está pronta" no `CLAUDE.md`;
- não há nenhuma validação no servidor: um PR pode ser mergeado sem que ninguém tenha
  rodado nada, porque não existe pipeline;
- o repo lida com DSNs, credenciais de container e dumps de banco; nada impede que uma
  credencial real entre no histórico por acidente;
- o único "lint" hoje é a convenção informal `gofmt`/`go vet`, sem staticcheck, errcheck
  ou detecção de código morto — categorias inteiras de bug passam batido.

O autor precisa de confiança de que "nada quebra com implementações novas" sem depender
de lembrar de rodar quatro comandos na mão antes de cada commit e push.

## Solution

O repositório passa a ter **gates de qualidade versionados** com um único ponto de
entrada, consumido por três adaptadores finos:

1. **Ponto de entrada único** — um script versionado (`scripts/check`) com dois níveis:
   - `fast` — formatação, lint estático, secrets scan e testes unitários (sem Docker);
   - `full` — tudo do `fast` mais a suite de conformidade com tag `docker`.
2. **`pre-commit`** → chama `scripts/check fast`. Barato o suficiente para não
   desencorajar commits pequenos.
3. **`pre-push`** → chama `scripts/check full`. Se o daemon Docker não responder, o push
   **falha explicitamente** — sem skip silencioso.
4. **Pipeline no GitHub Actions** → roda exatamente o mesmo `scripts/check full`, mais as
   checagens que só fazem sentido com rede/servidor (`govulncheck`, build do binário,
   verificação de `go.mod` limpo). É a rede de segurança para quem usou `--no-verify` ou
   não ativou os hooks.

Toolchain 100% Go: as ferramentas externas (`golangci-lint`, `gitleaks`, `govulncheck`)
são pinadas como *tool dependencies* no próprio `go.mod`, instaláveis com um comando e
idênticas em máquina local e CI. Sem Node, Husky, npm ou Prettier.

## User Stories

### Gate rápido no commit

1. Como contribuidor, quero que um commit seja bloqueado se algum arquivo Go não estiver
   formatado com `gofmt`, para o estilo do repo permanecer uniforme sem revisão manual.
2. Como contribuidor, quero que a falha de formatação liste **quais arquivos** divergem e
   o comando de correção (`gofmt -w`), para consertar sem adivinhar.
3. Como contribuidor, quero que o commit seja bloqueado se `go vet ./...` apontar
   problemas, para erros estáticos óbvios não entrarem no histórico.
4. Como contribuidor, quero que o commit seja bloqueado se o `golangci-lint` acusar
   problemas (staticcheck, errcheck, ineffassign, unused), para erros ignorados e código
   morto não se acumularem nas seis engines.
5. Como contribuidor, quero que erros de retorno ignorados nas engines (`defer rows.Close()`,
   `tx.Rollback()`, chamadas de driver) sejam apontados pelo linter, para falhas de restore
   não passarem silenciosas.
6. Como contribuidor, quero que o commit seja bloqueado se `go test ./...` falhar, para
   regressões de detecção, heurística de coluna de ordenação e lógica sem container não
   avançarem.
7. Como contribuidor, quero que o commit seja bloqueado se um segredo (senha, token, DSN
   com credencial real) for detectado no que estou commitando, para credenciais não
   entrarem no histórico do git.
8. Como contribuidor, quero que o gate rápido termine em poucos segundos, para eu manter
   o hábito de commits frequentes e pequenos.
9. Como contribuidor, quero uma mensagem final dizendo exatamente qual etapa falhou
   (format / vet / lint / secrets / unit), para saber o que corrigir sem ler o log inteiro.
10. Como contribuidor, quero que a saída bruta de `go test`/`go vet` continue visível no
    terminal, para depurar como se tivesse rodado o comando na mão.

### Gate completo no push

11. Como contribuidor, quero que o push rode todas as checagens do pre-commit de novo,
    para não empurrar um branch que passou num commit antigo mas ficou inconsistente depois.
12. Como contribuidor, quero que o push seja bloqueado se `go test -tags docker ./...`
    falhar, para conformidade e integração das engines nunca saírem quebradas do laptop.
13. Como mantenedor, quero que uma engine nova só possa ser empurrada depois de passar na
    suite genérica de conformidade, para o critério de "engine pronta" do `CLAUDE.md` virar
    barreira executável em vez de convenção.
14. Como contribuidor, quero que o pre-push verifique o daemon Docker **antes** de começar
    a suite pesada e falhe com mensagem explícita se ele estiver parado, para não achar que
    o push "passou limpo" sem a camada de integração.
15. Como contribuidor em emergência, quero que `git push --no-verify` continue existindo
    como válvula consciente, sabendo que o pipeline vai pegar o problema depois.

### Pipeline no GitHub

16. Como mantenedor, quero um workflow que rode em todo push para `main` e em todo pull
    request, para nenhuma mudança ser mergeada sem validação independente da máquina de quem
    escreveu.
17. Como mantenedor, quero que o pipeline execute o **mesmo** ponto de entrada dos hooks,
    para não existirem dois conjuntos divergentes de comandos entre local e CI.
18. Como mantenedor, quero que o pipeline rode a suite Docker completa nos runners, para
    quem usou `--no-verify` ou não ativou os hooks ainda seja pego.
19. Como mantenedor, quero que o pipeline rode `govulncheck`, para CVEs conhecidas em
    `pgx`, `mongo-driver`, `go-redis`, `go-sql-driver/mysql` e `modernc.org/sqlite` aparecerem
    sem eu precisar acompanhar advisories na mão.
20. Como mantenedor, quero que o pipeline falhe se `go.mod`/`go.sum` estiverem sujos
    (`go mod tidy` produziria diff), para dependências fantasma não se acumularem.
21. Como mantenedor, quero que o pipeline compile o binário (`go build`), para o pacote
    `main` — que os testes não exercitam por completo — não quebrar sem ninguém notar.
22. Como mantenedor, quero que as etapas rápidas (lint, unit) rodem separadas da suite
    Docker no relatório do PR, para ver num relance se a falha é de estilo ou de integração.
23. Como mantenedor, quero cache de módulos Go e de build no pipeline, para o feedback do
    PR chegar em minutos em vez de dezenas.
24. Como mantenedor, quero que o pipeline tenha um timeout explícito, para um container de
    engine travado não consumir minutos de runner indefinidamente.
25. Como mantenedor, quero que a versão de Go usada no CI venha do `go.mod` em vez de ser
    escrita à mão no workflow, para não haver divergência silenciosa entre local e CI.
26. Como mantenedor, quero poder disparar o pipeline manualmente (`workflow_dispatch`),
    para revalidar `main` sem precisar de um commit vazio.

### Setup e reprodutibilidade

27. Como contribuidor num clone novo, quero um comando único de setup que ative os hooks
    versionados, para não copiar arquivo na mão para `.git/hooks`.
28. Como contribuidor, quero que os scripts de hook vivam dentro do repositório, para
    mudanças nos gates viajarem com o código e todo mundo rodar a mesma coisa.
29. Como contribuidor, quero que as versões de `golangci-lint`, `gitleaks` e `govulncheck`
    sejam pinadas no repo, para o gate não passar na minha máquina e falhar na do outro por
    diferença de versão de linter.
30. Como contribuidor, quero instalar todas as ferramentas do gate com um comando só, para
    não caçar instruções de instalação de três projetos diferentes.
31. Como contribuidor, quero que o script avise claramente qual ferramenta está faltando e
    como instalá-la, se eu rodar o gate antes do setup.
32. Como contribuidor, quero rodar o mesmo conjunto de checagens **fora** dos hooks (`scripts/check fast`
    ou `full` na mão), para validar antes mesmo de tentar commit.
33. Como mantenedor, quero que a ativação seja local ao clone (nunca `git config --global`),
    para não afetar outros repositórios da máquina.
34. Como agente trabalhando neste repo, quero que o `CLAUDE.md` documente o ponto de entrada
    do gate na seção Commands, para eu usar o mesmo caminho que o humano em vez de inventar
    outro fluxo.
35. Como contribuidor novo, quero que o README explique o que cada hook roda, o que exige
    Docker e como pular em emergência, para entender o custo de cada operação de git.

### Comportamento em edge cases

36. Como contribuidor, quero que o gate rode a suite inteira independentemente de quais
    paths estão staged, para mudanças em `.scratch/` ou docs não liberarem um tree já
    quebrado por outra alteração.
37. Como contribuidor, quero que o gate pare na primeira etapa que falhar, para ter
    feedback rápido em vez de esperar a suite Docker depois de um erro de formatação.
38. Como contribuidor, quero que o gate funcione a partir de qualquer subdiretório do repo,
    para o hook não depender do diretório de onde rodei o `git commit`.
39. Como contribuidor, quero que o scan de segredos ignore o diretório `data/`
    (já gitignored, contém dumps reais), para não gastar tempo nem gerar ruído sobre
    arquivos que nunca serão commitados.
40. Como contribuidor, quero que credenciais de teste hardcoded nos arquivos de engine
    (senhas de container efêmero em `postgres.go`, `mysql.go`, etc.) não gerem falso positivo
    permanente no scan, via allowlist versionada e comentada.

## Implementation Decisions

- **Um ponto de entrada, três consumidores.** `scripts/check` recebe um nível (`fast` |
  `full`). `.githooks/pre-commit` chama `fast`; `.githooks/pre-push` chama `full`; o job
  do GitHub Actions chama `full`. Os hooks e o workflow são adaptadores finos — nenhuma
  lista de comandos duplicada. É a mesma disciplina de costura única aplicada em
  `engine.go`, agora no fluxo de build.
- **Composição do nível `fast`**, nesta ordem, parando na primeira falha:
  1. `gofmt -l` sobre o módulo — exit ≠ 0 se houver qualquer saída, listando os arquivos;
  2. `go vet ./...`;
  3. `golangci-lint run`;
  4. secrets scan (`gitleaks`) sobre o conteúdo a ser commitado;
  5. `go test ./...` (sem tag `docker`).
- **Composição do nível `full`**: `fast` + preflight do daemon Docker (`docker info`) +
  `go test -tags docker ./...`. O preflight vem **antes** da suite e falha com mensagem
  em pt-BR pedindo o daemon; nunca pula em silêncio.
- **Ferramentas pinadas via `go.mod`.** `golangci-lint`, `gitleaks` e `govulncheck` são
  módulos Go; registrar como tool dependencies (diretiva `tool` do Go 1.26, `go get -tool`)
  e invocar via `go tool <nome>`. Isso dá versão única para máquina local e CI, instalação
  com um comando, e zero runtime fora do Go. Se alguma delas se mostrar inviável como
  tool dependency, o fallback é `go run <module>@<versão-pinada>` com a versão numa
  constante única do script — nunca `@latest`.
- **Configuração do linter.** Um `.golangci.yml` versionado, habilitando ao menos
  `staticcheck`, `errcheck`, `ineffassign`, `unused`, `govet`. Nada de linters de estilo
  opinativo (nomes, comentários obrigatórios) — os comentários deste repo são em pt-BR e
  regras de doc-comment em inglês só gerariam ruído. Arquivos `_test.go` podem ter
  `errcheck` relaxado.
- **Configuração do secrets scan.** Um `.gitleaks.toml` versionado com allowlist explícita
  e comentada para as credenciais efêmeras de container usadas pelas engines e pelos testes,
  além de `data/` e `testdata/`. A allowlist é parte do contrato: se uma engine nova
  hardcodar senha de container, o autor adiciona a entrada conscientemente.
- **`govulncheck` só no CI.** Precisa de rede para consultar a base de vulnerabilidades;
  colocá-lo no pre-commit tornaria commits offline impossíveis. Fica como step do workflow.
- **Sem `go test -race` nesta entrega.** Aumenta o tempo da suite (que já paga containers
  no push) sem demanda concreta hoje; pode virar um step opcional do CI depois.
- **Sem formatação automática de arquivos staged.** O gate só verifica; o contribuidor
  aplica `gofmt -w` conscientemente. Evita diff-surpresa no meio de um commit.
- **Sem filtro por paths staged.** Módulo pequeno, package único; filtrar traria
  complexidade sem ganho e abriria porta para falso verde.
- **Ativação dos hooks.** `core.hooksPath` já aponta para `.githooks/` neste clone; o setup
  documentado (`scripts/install-hooks` ou instrução direta no README) roda
  `git config core.hooksPath .githooks` — local ao repo, nunca global.
- **Workflow do GitHub Actions**, arquivo único em `.github/workflows/`:
  - gatilhos: `push` em `main`, `pull_request`, e `workflow_dispatch`;
  - dois jobs — um rápido (`fast` + `govulncheck` + `go mod tidy` limpo + `go build`) e um
    pesado (suite `-tags docker`) — para o relatório do PR distinguir estilo de integração;
  - versão de Go derivada do `go.mod` (`go-version-file`), nunca hardcoded;
  - cache de módulos e de build habilitado;
  - `timeout-minutes` explícito no job pesado;
  - runner `ubuntu-latest`, que já traz o daemon Docker — os testes de conformidade sobem
    os containers de engine por conta própria, sem `services:` no workflow (a suite é dona
    do ciclo de vida dos containers, como já é hoje).
- **Verificação de `go.mod` limpo**: rodar `go mod tidy` e falhar se `git diff --exit-code`
  acusar mudança. Só no CI — evita mexer em arquivos do contribuidor durante um commit.
- **Idioma.** Mensagens de erro voltadas ao humano em pt-BR (alinhado a README e comentários
  do código); nomes de arquivos, alvos e steps em inglês.
- **Documentação.** README ganha uma seção "Qualidade / hooks" (o que cada gate roda, o que
  exige Docker, como ativar, como pular em emergência). `CLAUDE.md` ganha `scripts/check fast`
  e `scripts/check full` na seção Commands, para agentes usarem o mesmo caminho.
- **Blast radius.** Nenhum arquivo de engine é tocado. Se o `golangci-lint` acusar problemas
  pré-existentes nas engines, a correção é mínima e local (tratar o erro ou anotar exclusão
  justificada) — não é gatilho para refatorar engine.

## Testing Decisions

- **O que é um bom teste aqui**: comportamento externo do gate — dado um estado da árvore,
  qual é o exit code e qual mensagem sai. Não testar o miolo do script (quais flags passa
  para cada ferramenta), que é detalhe de implementação e muda.
- **Módulos sob teste**: apenas `scripts/check`, os hooks e o workflow. O domínio
  `Engine`/`Session` **não** é retestado por esta feature — a suite existente é o *sujeito*
  invocado pelo gate, não o objeto do teste.
- **Casos de aceitação do script** (verificáveis num teste shell mínimo ou no smoke manual
  documentado no ticket):
  1. árvore saudável + `fast` → exit 0;
  2. arquivo Go desformatado → exit ≠ 0, mensagem nomeando o arquivo e `gofmt -w`;
  3. teste unitário quebrado de propósito → exit ≠ 0, etapa identificada como unit;
  4. achado do linter (ex.: erro de retorno ignorado) → exit ≠ 0, etapa identificada como lint;
  5. segredo plantado num arquivo staged → exit ≠ 0, etapa identificada como secrets;
  6. `full` com daemon Docker parado → exit ≠ 0 **antes** de a suite pesada começar,
     mensagem explícita sobre Docker;
  7. `full` com Docker ok e suite verde → exit 0;
  8. script invocado a partir de um subdiretório → mesmo resultado que da raiz.
- **Casos de aceitação dos hooks**: commit com árvore suja aborta; commit limpo passa; push
  com Docker parado aborta; push com suite verde passa. Smoke manual documentado no ticket
  de implementação é suficiente aqui — hook é adaptador de três linhas.
- **Caso de aceitação do pipeline**: abrir um PR de teste com um defeito plantado (um por
  categoria: formatação, lint, unit, docker) e conferir que o job correspondente fica
  vermelho; PR limpo fica verde.
- **Prior art no repo**: as duas camadas de teste já documentadas no `CLAUDE.md`
  (`go test ./...` e `go test -tags docker ./...`), `conformance_test.go` como corpo genérico
  parametrizado por `Engines()`, e as fixtures de detecção em `testdata/headers/`. O gate
  não introduz camada nova de teste — orquestra as existentes.

## Out of Scope

- `go test -race`, testes de mutação, benchmarks ou gates de performance.
- Métrica de cobertura mínima obrigatória (nem coleta nem threshold).
- Publicação de artefatos, release automatizado, cross-compilação, matriz de SO/versões de Go.
- Deploy, container image do próprio `db-verify`, ou push para registry.
- Hooks além de `pre-commit` e `pre-push` — nada de `commit-msg`, conventional commits ou
  validação de mensagem.
- Assinatura de commits (GPG/sigstore), CODEOWNERS, branch protection rules e políticas de
  review no GitHub (configuração de repositório, não de código).
- Dependabot / atualização automática de dependências.
- Bloquear `--no-verify` ou alterar os guardrails de agente em `.claude/settings.json`.
- Qualquer toolchain Node (Husky, lint-staged, Prettier) — o projeto é Go puro.
- Skip condicional da suite Docker no pre-push quando o daemon está parado.
- Refatorar engines para agradar o linter além do mínimo necessário para o gate ficar verde.

## Further Notes

- Esta spec substitui a de `.scratch/git-hooks-quality-gates/` (removida em `b0447d1`),
  ampliando o escopo com pipeline no GitHub, lint estático de verdade, secrets scan e
  varredura de vulnerabilidades. A decisão anterior de "sem linter além de gofmt/vet" fica
  revogada — a seção de lint do `CLAUDE.md` precisa ser atualizada junto.
- `core.hooksPath` já está configurado para `.githooks/` neste clone, mas o diretório está
  vazio; num clone novo o setup ainda é necessário.
- A assimetria de custo é proposital: commit paga segundos, push paga containers, PR paga o
  runner. Quem itera rápido não é punido; quem publica é validado a sério.
- O pipeline é rede de segurança, não a barreira primária: o objetivo é que o CI raramente
  fique vermelho, porque o pre-push já pegou. Se o CI virar o único gate que pega coisas,
  é sinal de que os hooks não estão ativos nos clones.
