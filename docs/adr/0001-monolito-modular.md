# ADR 0001: Monólito modular

Status: aceito (2026-09-30)

## Contexto

O código do db-verify morava em cerca de 45 arquivos no pacote `main`: interface
do terminal, acesso ao Docker, detecção, seis engines e seus testes, lado a lado.
Como tudo compartilhava um pacote, a regra de arquitetura ("quem chama depende só
de `Engine`/`Session` e do registro, nunca de uma engine concreta") existia só no
`CLAUDE.md`: qualquer arquivo enxergava o que era privado de qualquer outro, e nada
reprovava um acoplamento indevido. As engines se cadastravam por `init()`
espalhados, então a lista de engines e a ordem de desempate da detecção dependiam
da ordem alfabética dos arquivos.

## Decisão

O projeto continua sendo um binário com um `go.mod`, mas o código passa a morar em
pacotes com fronteiras garantidas pelo compilador. Cada escolha abaixo tem um
motivo, registrado para que ninguém a "conserte" depois.

- **Pacotes em `internal/` num único `go.mod`.** `engine`, `relational`, `docker`,
  `dumpio`, `detect`, `ui` e `conformance` ficam em `internal/`; as engines, em
  `engines/<nome>/`. Um `go.mod` só mantém `go build -o db-verify .`, `./...` e
  `scripts/check` como estavam. Um módulo por engine e mover o `main` para `cmd/`
  ficaram de fora: custo sem ganho para um binário só.
- **MySQL e MariaDB no mesmo pacote (`engines/mysql`).** O MariaDB reaproveita
  container, sessão, SQL de introspecção, formatação e resolução de versão do MySQL.
  Separar obrigaria a exportar esses helpers só para o vizinho usar.
- **Lista explícita no lugar de `init()`.** `engines.go`, na raiz, chama
  `engine.Register` para cada engine, na ordem mariadb, mongo, mysql, postgres,
  redis, sqlite, que é a ordem que o desempate de detecção já tinha (ordem
  alfabética dos arquivos). Assim a lista e a ordem ficam escritas num lugar só,
  renomear arquivo não muda o resultado da detecção, e a ordem só muda por decisão
  deliberada, porque ela decide o desempate entre engines de mesma confiança.
  É um inicializador de variável de pacote, não um `init()` nem um trecho do
  `main()`, para rodar também nos testes da raiz, onde `main()` não executa.
- **Fixture de conformidade com build tag `docker`, dentro do pacote da engine.**
  Um `_test.go` não é compilado quando outro pacote o importa, então a fixture não
  poderia morar ali e ser achada pela suíte da raiz. Um arquivo `conformance.go` com
  a tag `docker` é compilado só nos testes com Docker e fica fora do binário de
  produção. Ele registra a fixture em `internal/conformance`, e a suíte continua
  reprovando engine da lista sem fixture: a trava "engine sem fixture reprova" foi
  preservada.
- **Testes do conjunto montado na raiz.** Conformidade genérica sobre `Engines()`,
  `InspectDump` contra engines reais e empate de detecção precisam de todas as
  engines registradas; ficam ao lado da lista em `engines.go`. Testes de uma engine
  moram no pacote dela e continuam enxergando o que é privado dela; testes de
  detecção com engine falsa moram em `detect`.
- **`run()` no `main`, sem `internal/app`.** Com um único ponto de entrada, um
  pacote de orquestração seria só indireção. O `main.go` se lê de cima a baixo:
  flags, detecção, provisionamento, TUI, fechamento, dica de conexão.
- **`testdata/headers/` único na raiz.** O teste de empate de detecção precisa
  enxergar todos os cabeçalhos juntos. Os pacotes chegam lá por
  `conformance.HeaderPath`, sem `../../../` repetido.
- **`depguard` como fiscal das fronteiras.** O compilador esconde o que é privado,
  mas não impede uma importação pública na direção errada. As regras no
  `.golangci.yml`, rodadas por `scripts/check fast`, reprovam: engine importando
  outra engine, `ui` ou `detect`; `ui` e `detect` importando qualquer engine
  concreta; `engine` e `dumpio` importando outro pacote do projeto. Foi ligado no
  último passo da migração, com todos os pacotes já existentes, para não precisar
  de exceções temporárias.
- **Achado 10 deixado para depois.** O mapa de dívida técnica pede Docker só via
  `DockerHost` e injeção do host no `Provision`. Isso muda interfaces e
  comportamento, e esta migração é puramente mecânica (mover e exportar o
  necessário). Os helpers globais do Docker continuam existindo, exportados, em
  `internal/docker`, até esse achado ser tratado sobre a fronteira já clara.

## Consequências

- A raiz tem só o `main`, a lista de engines e os testes do conjunto montado; o
  resto está organizado por responsabilidade.
- Uma engine nova é um pacote em `engines/`, uma fixture com tag `docker` e uma
  linha na lista de `engines.go`. Esquecer a fixture reprova a suíte; importar outra
  engine reprova o `check fast`.
- Nenhum comportamento visível ao operador mudou: mesma detecção, mesmas
  mensagens, mesmos containers, mesma TUI.
- Identificadores que passaram a ser usados entre pacotes viraram exportados
  (progresso de `ProvisionOpts`, helpers de porta do Docker). É o custo de fronteira
  real, limitado ao necessário para compilar.
- Adicionar engine exige editar `engines.go`, um ponto central a mais do que o
  `init()` exigia. É o preço de ter a ordem de desempate escrita.
- Código comum entre engines não pode ser copiado nem importado de uma engine para
  outra: sobe para `relational`, `docker` ou `dumpio`.
