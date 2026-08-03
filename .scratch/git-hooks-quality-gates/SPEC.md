# Gates de qualidade via pre-commit e pre-push

Status: ready-for-agent

## Problem Statement

O `db-verify` está crescendo com várias engines (Postgres, MySQL, MariaDB, SQLite,
Redis, MongoDB), cada uma com detecção, provisionamento, restore e TUI atrás da
mesma costura `Engine`/`Session`. Hoje não há nenhuma barreira automática no fluxo
local de git: um commit ou push pode sair com código mal formatado, `go vet` falhando,
testes unitários quebrados ou — pior — a suite de conformidade Docker (`go test
-tags docker`) vermelha, e o problema só aparece depois, quando outra pessoa (ou o
próprio autor) roda os testes na mão.

O operador e o contribuídor precisam de confiança de que "nada quebra com
implementações novas" sem lembrar de rodar `gofmt`, `go vet` e as duas camadas de
teste antes de cada commit/push. Os hooks sample do `.git/hooks/` não estão ativos
nem versionados; o README e o `CLAUDE.md` documentam os comandos, mas ninguém os
executa por obrigação.

## Solution

O repositório passa a ter **gates de qualidade versionados** no fluxo git:

1. Um **ponto único de entrada** (script ou alvo Make versionado no repo) que orquestra
   as checagens já convencionadas do projeto — sem reinventar lint nem duplicar a
   lógica nos hooks.
2. Um **pre-commit** rápido: formatação (`gofmt`), análise estática (`go vet`) e
   testes unitários sem Docker (`go test ./...`).
3. Um **pre-push** mais pesado: as mesmas checagens do pre-commit **mais** a suite
   completa com tag Docker (`go test -tags docker ./...`). Se o daemon Docker não
   estiver disponível, o push **falha de forma explícita** — não silencia a barreira
   de integração.
4. Um **setup documentado e repetível** para apontar o git do clone para os hooks
   versionados (`core.hooksPath` ou equivalente), para que todo contribuídor ative
   os gates com um comando.

Stack nativa Go/shell — sem Husky, npm, Prettier ou Node. Falha de qualquer etapa
aborta o commit/push e imprime claramente qual comando falhou.

## User Stories

### Gates no dia a dia

1. Como contribuídor, quero que um commit seja bloqueado se o código Go não estiver
   formatado com `gofmt`, para o estilo do repo permanecer uniforme sem revisão manual.
2. Como contribuídor, quero que um commit seja bloqueado se `go vet ./...` apontar
   problemas, para erros estáticos óbvios não entrem no histórico.
3. Como contribuídor, quero que um commit seja bloqueado se `go test ./...` falhar,
   para regressões unitárias (detecção, heurísticas, lógica sem container) não
   avancem.
4. Como contribuídor, quero que o pre-commit seja rápido o bastante para não me
   desencorajar de fazer commits pequenos, para eu manter o hábito de commits
   frequentes.
5. Como contribuídor, quero que um push seja bloqueado se a suite `go test -tags
   docker ./...` falhar, para a conformidade e integração das engines não sejam
   publicadas quebradas.
6. Como contribuídor, quero que o pre-push também rode as checagens do pre-commit,
   para eu não empurrar um branch que passou do commit antigo mas ficou inconsistente
   depois.
7. Como contribuídor, quero uma mensagem de erro clara dizendo qual etapa falhou
   (format / vet / unit / docker), para eu saber o que corrigir sem adivinhar.
8. Como contribuídor, quero que, se o Docker não estiver rodando no pre-push, o hook
   falhe com uma mensagem explícita pedindo o daemon, para eu não achar que o push
   "passou limpo" sem a suite de integração.
9. Como contribuídor, quero poder rodar o mesmo conjunto de checagens **fora** dos
   hooks (via o ponto de entrada versionado), para validar mudanças antes mesmo de
   tentar commit/push.
10. Como contribuídor em emergência, quero ainda poder usar `--no-verify` do git
    quando eu deliberadamente precisar contornar (consciente do risco), sem que o
    projeto remova essa válvula do git — os hooks só são o caminho feliz.

### Setup e versionamento

11. Como contribuídor novo no clone, quero um comando único de setup (documentado no
    README) que ative os hooks versionados no meu `.git`, para eu não precisar
    copiar arquivos na mão.
12. Como contribuídor, quero que os scripts de hook vivam **dentro do repositório**
    (não só em `.git/hooks` local), para mudanças nos gates viajem com o código e
    todo mundo rode a mesma coisa.
13. Como mantenedor, quero que o setup use apenas ferramentas já esperadas no
    ambiente Go/Docker do projeto (`go`, `gofmt`, `docker`), para não introduzir
    runtime Node ou gerenciador de pacotes paralelo.
14. Como mantenedor, quero que o README (e, se necessário, o `CLAUDE.md`) digam o
    que cada hook roda e como ativá-los, para agentes e humanos não inventarem outro
    fluxo.
15. Como mantenedor, quero que a ativação dos hooks seja local ao clone (não altere
    `git config --global`), para não interferir em outros repositórios da máquina.

### Comportamento em edge cases

16. Como contribuídor, quero que commits que não tocam código Go ainda passem pelas
    checagens de teste do módulo (o pacote inteiro), para documentação/tickets em
    `.scratch/` não liberarem um tree já quebrado por outra mudança não commitada —
    ou, se o desenho optar por pular quando não há `.go` staged, que isso esteja
    documentado de forma explícita. **Decisão desta spec: sempre rodar as
    checagens do ponto de entrada no pre-commit/pre-push, independente dos paths
    staged** — o módulo é pequeno e a suite unitária é barata; evita falso verde.
17. Como contribuídor, quero que a saída dos testes/vet continue legível no terminal
    (stdout/stderr dos comandos Go), para depurar como se eu tivesse rodado na mão.
18. Como contribuídor, quero que o pre-push não rode se as refs sendo empurradas não
    incluem commits novos relevantes — comportamento padrão do git pre-push é
    suficiente; não precisa de lógica extra de "skip se só tags".
19. Como operador de CI futura (fora de escopo agora), quero que o mesmo ponto de
    entrada dos hooks seja reutilizável por um job remoto, para não haver dois
    conjuntos de comandos divergentes.

### Qualidade e confiança

20. Como mantenedor, quero que implementações novas de engine sejam forçadas a
    passar na suite de conformidade no pre-push, para a regra "a engine está pronta
    quando passa na suite genérica" seja aplicada antes do código sair do laptop.
21. Como mantenedor, quero que o gate não introduza linters além de `gofmt`/`go vet`
    nesta entrega, alinhado à convenção atual do repo ("não há lint config
    separado").
22. Como mantenedor, quero que falhas de formatação indiquem **quais arquivos**
    divergem do `gofmt` (ou um comando de correção), para o contribuídor consertar
    rápido com `gofmt -w`.

## Implementation Decisions

- **Um único ponto de entrada de qualidade.** Preferir um script shell versionado
  (ex.: sob `scripts/`) e/ou alvos Make que encapsulem as etapas. Os arquivos de
  hook (`pre-commit`, `pre-push`) só chamam esse ponto — sem duplicar listas de
  comandos. Ideal: um alvo "rápido" (format + vet + unit) e um alvo "completo"
  (rápido + docker), ou um script com subcomandos/`CHECK_LEVEL`.
- **Conteúdo do gate rápido (pre-commit):**
  1. Verificar formatação: `gofmt -l` nos pacotes/arquivos Go do módulo; exit ≠ 0
     se houver saída.
  2. `go vet ./...`
  3. `go test ./...` (sem tag `docker` — suite que roda em qualquer máquina com Go).
- **Conteúdo do gate completo (pre-push):** gate rápido + `go test -tags docker
  ./...`. Antes da suite Docker, verificar que o daemon responde (ex.: `docker
  info`); se não, falhar com mensagem pedindo Docker ligado — **não** pular em
  silêncio.
- **Ativação dos hooks:** versionar os hooks no repo (diretório dedicado, ex.
  `.githooks/`) e documentar `git config core.hooksPath <dir>` (local ao repo)
  como setup. Alternativa aceitável: script `scripts/install-hooks` que configura
  isso. Não depender de cópia manual para `.git/hooks`.
- **Sem Node/Husky/lint-staged/Prettier.** O projeto é Go puro; a skill genérica de
  Husky não se aplica.
- **Sem `golangci-lint` nesta entrega.** Manter `gofmt` + `go vet` como único lint,
  coerente com `CLAUDE.md`.
- **Sem formatação automática no stage** (não reescrever arquivos no pre-commit). O
  gate só verifica; o contribuídor aplica `gofmt -w` conscientemente. Evita diffs
  surpresa no meio do commit.
- **Ordem das etapas:** format → vet → testes (unit, depois docker no push). Parar
  na primeira falha (`set -e` ou equivalente) para feedback rápido.
- **Documentação:** atualizar README com seção "Hooks / qualidade local" (como
  ativar, o que roda em cada hook, necessidade de Docker no push). Atualizar
  `CLAUDE.md` Commands com o ponto de entrada (ex.: `make check` / `scripts/check`)
  para agentes usarem o mesmo caminho.
- **Não alterar `git config` global** nem políticas deny de agentes; `--no-verify`
  continua sendo válvula humana de emergência (agentes deste repo já têm
  restrições próprias via guardrails).
- **Binário `db-verify` e artefatos locais** não entram nos gates além do que `go
  test`/`go vet` já cobrem; não é necessário `go build` separado se os testes de
  pacote já compilam o código — opcional incluir `go build -o /dev/null .` no gate
  rápido se quiser falhar em `main` sem testes. Preferência: **não** exigir build
  separado se `go test ./...` já compila os pacotes testáveis; o pacote `main` é
  exercido pelos testes do módulo na prática via compilação dos arquivos — se o
  `main` não for coberto pela compilação dos testes, incluir `go build -o
  /dev/null .` no gate rápido.
- **Idioma:** mensagens de erro voltadas ao humano nos scripts podem ser em pt-BR,
  alinhado ao README; nomes de alvos/arquivos em inglês (`check`, `pre-commit`,
  etc.).

## Testing Decisions

- **Bom teste aqui é comportamento externo do gate**, não o miolo interno dos
  engines: "rodar o ponto de entrada com a árvore saudável → exit 0"; "quebrar de
  propósito um teste unitário → exit ≠ 0"; "simular Docker down → pre-push/completo
  falha com mensagem sobre Docker".
- **Não** re-testar a suite de conformidade das engines dentro desta feature — ela
  já é o *sujeito* invocado pelo hook. Prior art: `go test ./...` e `go test -tags
  docker ./...` documentados em `CLAUDE.md`; fixtures em `testdata/headers/` e
  `conformance_test.go`.
- **Verificação manual / smoke da feature de hooks:**
  1. Ativar `core.hooksPath` conforme o setup.
  2. Commit com formatação quebrada → deve abortar.
  3. Commit com tree limpa → deve passar (unitários verdes).
  4. Push com Docker parado → deve abortar com mensagem clara.
  5. Push com Docker ok e suite verde → deve passar.
- Se for útil, um teste shell mínimo (ou alvo `make test-hooks`) que invoca o
  script de check em modo unitário e asserta exit codes — opcional; não é
  obrigatório se o smoke manual estiver documentado no ticket de implementação.
- **Módulos sob teste nesta feature:** apenas os scripts/hooks/Make — não o domínio
  `Engine`/`Session`.

## Out of Scope

- CI remota (GitHub Actions, etc.) — o ponto de entrada deve ser *reutilizável*
  depois, mas configurar o pipeline não faz parte desta spec.
- `golangci-lint`, staticcheck, revive, ou qualquer linter além de `gofmt`/`go vet`.
- Formatação automática / rewrite de staged files no pre-commit.
- Husky, npm, Prettier, lint-staged, ou qualquer toolchain Node.
- Hooks além de `pre-commit` e `pre-push` (commit-msg, prepare-commit-msg, etc.).
- Política de bloquear `--no-verify` ou alterar guardrails de agentes.
- Skip condicional da suite Docker quando o daemon está down (deve falhar).
- Testes de performance ou timeouts especiais nos hooks (usar timeouts padrão do
  `go test`; se a suite Docker for longa, isso é aceitável no push).
- Assinar commits, secrets scanning, ou outros gates de segurança.

## Further Notes

- Costura acordada: **um** ponto de entrada versionado; hooks são adaptadores
  finos. Ideal de "uma costura" respeitado — não espalhar `go test`/`gofmt` em
  vários lugares.
- A suite Docker no pre-push é intencionalmente a barreira pesada: quem empurra
  código de engine precisa do daemon e da conformidade verde. Commits locais
  frequentes ficam com o gate barato.
- Há trabalho paralelo de multi-engine em
  `.scratch/multi-engine-backup-verification/`; esta feature é ortogonal e pode
  aterrissar a qualquer momento sem depender da conclusão daquela.
- Ao implementar, preferir boy scout mínimo: não refatorar engines; só adicionar
  scripts, hooks, docs de ativação.
