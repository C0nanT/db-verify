# Monólito modular: pacotes Go com fronteiras garantidas pelo compilador

Status: ready-for-agent

Origem: sessão de grilling de 2026-09-29. A partição em módulos proposta em
`docs/tech-debt/README.md` vira fronteira real no código. Pré-requisito: o
ticket 06 de `.scratch/core-debt-fase1/` (desempate ordinal no Postgres)
fechado antes do primeiro passo.

## Problem Statement

Quem mantém o db-verify abre o repositório e encontra cerca de 45 arquivos
Go soltos na raiz, todos no mesmo pacote `main`: a interface do terminal, a
conversa com o Docker, a detecção de formato, as seis engines e os testes de
cada uma, lado a lado. Não dá para saber de relance quais peças existem, quem
depende de quem, nem por onde começar a ler.

A regra de arquitetura do projeto ("quem chama depende só de `Engine`/`Session`
e do registro, nunca de uma engine concreta") existe só no `CLAUDE.md`. Como
tudo está no mesmo pacote, qualquer arquivo enxerga o que é privado de
qualquer outro: a TUI pode chamar uma função interna do Postgres, o Redis pode
usar um helper do MySQL, e nada reprova isso. A regra é mantida por disciplina,
e cada mudança arrisca desgastá-la sem ninguém perceber.

As engines se cadastram por `init()` espalhados, então a lista de engines
suportadas e a ordem em que disputam a detecção não aparecem escritas em lugar
nenhum: dependem da ordem alfabética dos arquivos.

## Solution

O projeto continua sendo um binário só, com um `go.mod` só, mas o código passa
a morar em pacotes Go internos, cada um com uma responsabilidade: o contrato das
engines, a heurística relacional compartilhada, o acesso ao Docker, a detecção,
a interface do terminal, a suíte de conformidade e um pacote por engine (MySQL e
MariaDB juntos). O `main` fica com as flags, o roteiro do programa e uma lista
explícita das engines registradas.

O compilador passa a impedir que um pacote use o que é privado de outro, e uma
regra de lint (`depguard`) reprova importações que violem a direção das
dependências: engines não importam engines nem a interface; interface e
detecção não importam engines concretas.

A mudança é puramente mecânica: nenhum comportamento visível ao operador
muda, e `./scripts/check full` passa antes e depois de cada passo. A
documentação (`CLAUDE.md`, `README.md`, índice de dívida técnica e um ADR novo)
passa a descrever os pacotes reais.

## User Stories

1. Como mantenedor, quero ver na raiz do repositório só o `main` e uma pasta `internal/` organizada por responsabilidade, para entender em segundos quais peças o projeto tem.
2. Como mantenedor, quero que cada engine more no seu próprio pacote, para abrir uma pasta e ver tudo que diz respeito àquela engine (código, testes, fixture) e nada mais.
3. Como mantenedor, quero que o contrato `Engine`/`Session`, os tipos compartilhados e o registro morem num pacote próprio, para saber exatamente qual é a superfície que toda engine precisa implementar.
4. Como mantenedor, quero que a heurística de coluna de ordenação more num pacote relacional próprio, para que só as engines relacionais dependam dela e ela não pareça parte do contrato geral.
5. Como mantenedor, quero que o acesso ao Docker (`DockerHost`, portas, retry) more num pacote próprio, para que o achado 10 do mapa de dívida (Docker só via `DockerHost`) seja feito depois sobre uma fronteira clara.
6. Como mantenedor, quero que a detecção de formato more num pacote que conhece só o contrato, para poder testá-la com engines falsas sem depender de nenhuma engine real.
7. Como mantenedor, quero que o seletor de backups e a TUI morem num pacote de interface que conhece só o contrato, para mudar a tela sem risco de acoplar a uma engine.
8. Como mantenedor, quero que MySQL e MariaDB fiquem no mesmo pacote, para que o código que compartilham continue privado em vez de virar API exportada só para o vizinho usar.
9. Como mantenedor, quero abrir o `main.go` e ler de cima a baixo o roteiro do programa (flags → detecção → provisionamento → TUI → fechamento → dica de conexão), para entender o fluxo sem pular entre pacotes.
10. Como mantenedor, quero uma lista explícita de engines registradas no `main`, para ver num lugar só quais engines existem e em que ordem disputam a detecção.
11. Como mantenedor, quero que a ordem de registro das engines deixe de depender do nome dos arquivos, para que renomear um arquivo nunca mude o resultado de uma detecção.
12. Como mantenedor, quero que o compilador me impeça de usar o que é privado de outro pacote, para que a regra de arquitetura não dependa de disciplina.
13. Como mantenedor, quero que o `scripts/check fast` reprove uma engine que importe outra engine, a interface do terminal ou a detecção, para que o acoplamento proibido seja pego no pre-commit.
14. Como mantenedor, quero que o `scripts/check fast` reprove a interface do terminal ou a detecção importando uma engine concreta, para que elas continuem dependendo só do contrato.
15. Como mantenedor, quero que a suíte de conformidade continue sendo um único teste genérico sobre todas as engines registradas, sem `if` por nome de engine, para que "a engine está pronta" continue significando a mesma coisa para todas.
16. Como mantenedor, quero que uma engine registrada sem fixture de conformidade continue reprovando a suíte, para que uma engine nova não escape do contrato por esquecimento.
17. Como autor de uma engine nova, quero um roteiro curto (criar pacote, implementar o contrato, criar a fixture, adicionar à lista do `main`), para saber exatamente onde cada coisa vai.
18. Como autor de uma engine nova, quero que a fixture de conformidade fique no pacote da própria engine e só seja compilada com a tag `docker`, para que o binário de produção não carregue código de teste.
19. Como autor de uma engine nova, quero que o `depguard` me avise se eu importar outra engine, para reaproveitar código pelo lugar certo (pacote relacional ou Docker) em vez de copiar ou acoplar.
20. Como mantenedor, quero que os testes unitários de cada engine morem no pacote dela e continuem enxergando o que é privado dela, para não precisar exportar nada só para testar.
21. Como mantenedor, quero que os testes que precisam de todas as engines reais (detecção sobre backups reais, empate de detecção, conformidade) morem na raiz ao lado da lista de registro, para que o teste do conjunto montado fique junto da montagem.
22. Como mantenedor, quero que os cabeçalhos de exemplo continuem num único diretório `testdata/headers/` na raiz, para que o teste de empate de detecção enxergue todos juntos.
23. Como mantenedor, quero um helper que resolva o caminho de `testdata/headers/` a partir de qualquer pacote, para não espalhar `../../../` pelos testes.
24. Como mantenedor, quero que cada passo da migração deixe o projeto compilando e o `check full` verde, para poder parar em qualquer ponto sem deixar o repositório quebrado.
25. Como mantenedor, quero que a migração vá das folhas para cima (contrato, relacional e Docker primeiro; engines por último), para que cada passo só dependa de pacotes que já existem.
26. Como mantenedor, quero que o Postgres seja a última engine migrada, para não conflitar com o ticket 06, que mexe nele.
27. Como operador, quero que o db-verify se comporte exatamente como antes (mesma detecção, mesmas mensagens, mesmos containers, mesma TUI, mesma dica de conexão), para que a reorganização interna não me afete.
28. Como operador, quero que `go build -o db-verify .` e as flags continuem iguais, para que meus scripts e costumes não quebrem.
29. Como mantenedor, quero que `scripts/check` e os hooks continuem funcionando sem mudança, para não mexer no ponto de entrada dos gates.
30. Como mantenedor, quero que o `CLAUDE.md` descreva os pacotes reais, a lista explícita de registro, a fixture com build tag e a regra do `depguard`, para que agentes trabalhem sobre a estrutura atual e não sobre a antiga.
31. Como mantenedor, quero que o `README.md` troque a tabela de arquivos por uma tabela de pacotes e atualize o roteiro "como adicionar uma engine", para que quem chega ao projeto encontre a descrição correta.
32. Como mantenedor, quero que a tabela de módulos de `docs/tech-debt/README.md` passe a apontar para os pacotes e deixe de ser "proposta", para que as próximas revisões de dívida meçam contra a fronteira real.
33. Como mantenedor, quero um ADR registrando as decisões desta migração e os motivos, para que ninguém "conserte" depois uma escolha não óbvia (fixture com build tag, lista explícita, MySQL+MariaDB juntos, `run()` no `main`).
34. Como mantenedor, quero que as descrições do `.gitleaks.toml` e os comentários que citam nomes de arquivo antigos sejam atualizados junto com o passo que move aquele código, para que a documentação não aponte para arquivos que não existem mais.

## Implementation Decisions

### Pacotes

Todos sob `internal/`, no mesmo módulo Go. O `main` continua na raiz, então
`go build .` e `./...` não mudam.

| Pacote | Responsabilidade | Pode importar (do projeto) |
|---|---|---|
| `main` (raiz) | flags, roteiro `run()`, lista explícita de engines, testes do conjunto montado | todos |
| `engine` | interfaces `Engine`/`Session`, tipos compartilhados (`Match`, `Backup`, `ProvisionOpts`, `Collection`, `Health`, `ResultSet`, `ConnectHint`, `RestoreResult`…), registro (`Register`, `Engines`, `Lookup`) | nenhum |
| `relational` | heurística de coluna de ordenação e helpers de dialeto compartilhados | `engine` |
| `docker` | `DockerHost`: daemon, portas livres, retry por conflito de porta | `engine` se necessário |
| `detect` | leitura/descompressão do cabeçalho, disputa entre engines registradas, `InspectDump` | `engine` |
| `ui` | seletor de backups e TUI bubbletea | `engine`, `detect` |
| `conformance` | registro de fixtures, tipo `ConformanceFixture`, helpers da suíte, helper de caminho do `testdata` | `engine` (e `docker` se os helpers precisarem) |
| `engines/postgres`, `engines/mysql` (MySQL **e** MariaDB), `engines/sqlite`, `engines/redis`, `engines/mongo` | uma engine cada (MySQL/MariaDB compartilham) | `engine`, `relational`, `docker`, `conformance` (só nos arquivos com tag `docker`) |

- **MySQL e MariaDB no mesmo pacote:** o MariaDB reaproveita container,
  sessão, SQL de introspecção, formatação e resolução de versão do MySQL.
  Separar obrigaria a exportar esses helpers só para o vizinho.
- **Nomes exportados:** o que hoje é minúsculo e passa a ser usado de fora do
  pacote vira maiúsculo (por exemplo os métodos de progresso de
  `ProvisionOpts` e os helpers de porta do Docker). Nada além do necessário
  para compilar é exportado.
- **Helpers globais do Docker:** as funções de pacote presas ao
  `DockerHost` global continuam existindo, exportadas, até o achado 10 ser
  tratado. Injetar o `DockerHost` no `Provision` **não** faz parte deste spec.

### Registro das engines

- O `init()` + `Register` de cada engine sai. O `main` tem uma única lista
  explícita chamando o registro para cada engine, na ordem em que as engines
  hoje resultam registradas (ordem alfabética dos arquivos: mariadb, mongo,
  mysql, postgres, redis, sqlite), para que o desempate de detecção não mude.
- O registro em si (`Register`, `Engines`, `Lookup`) continua no pacote
  `engine`, com o mesmo comportamento.

### Suíte de conformidade

- A fixture de cada engine deixa de ser um `_test.go` e vira um arquivo de
  código do pacote da engine com build tag `docker`, que se registra no
  registro de fixtures do pacote `conformance`. Assim ela só é compilada nos
  testes com Docker e continua fora do binário de produção.
- O teste genérico continua um só, na raiz, iterando sobre `Engines()`, sem
  ramificação por nome de engine. Ele continua reprovando uma engine registrada
  sem fixture.
- Os helpers da suíte (`requireDocker`, `containerExists`, nome único,
  busca de coleção) vão para o pacote `conformance` com build tag `docker`.

### Orquestração

- `run()` continua no `main`. Não há `internal/app`: com um único ponto de
  entrada, ele seria só uma indireção.

### Fronteiras fiscalizadas

- Regra `depguard` no `.golangci.yml`, rodando pelo `scripts/check fast`
  como já acontece com o lint:
  - `engines/*` não importa outra engine, `ui`, `detect` nem `main`.
  - `ui` e `detect` não importam nenhum pacote de `engines/*`.
  - `engine` não importa nenhum outro pacote do projeto.
- O `depguard` é ligado no último passo, quando todos os pacotes já existem,
  para não precisar de exceções temporárias.

### Ordem da migração

Cada passo compila e deixa `./scripts/check full` verde.

1. `engine`, `relational`, `docker`. O código que continua na raiz passa a
   referenciar `engine.X`.
2. `detect` e `ui`.
3. `conformance` (registro de fixtures, helpers, helper de `testdata`). A
   suíte genérica continua na raiz.
4. Uma engine por passo: sqlite, redis, mongo, mysql+mariadb, postgres.
   Cada passo leva junto os testes unitários da engine, cria a fixture com
   build tag e troca o `init()` por uma entrada na lista explícita do `main`.
5. `depguard`, conferência final da lista explícita, e documentação (ver
   abaixo).

### Documentação

| Documento | Mudança | Passo |
|---|---|---|
| `CLAUDE.md` | *Architecture* (seam Engine/Session, fluxo de detecção, fluxo do `main`), *Testing tiers* e *SOLID → In this repo* passam a falar de pacotes, lista explícita, fixture com build tag e regra `depguard`. *Commands* não muda. | 5 |
| `README.md` | Tabela de arquivos vira tabela de pacotes; roteiro "como adicionar uma engine" vira: criar pacote em `engines/`, implementar o contrato, criar fixture com tag `docker`, adicionar à lista do `main`. | 5 |
| `docs/tech-debt/README.md` | Tabela de módulos aponta para os pacotes e deixa de ser `proposed`. O guardrail "CLI Docker só via `DockerHost`" ganha nota de que a checagem pode ser por pacote. | 5 |
| ADR 0001 em `docs/adr/` (pasta nova) | Monólito modular: as decisões desta seção e o porquê de cada uma. | 5 |
| `.gitleaks.toml` | Descrições que citam nomes de arquivo de engine. Só texto: as regras são por valor. | junto com cada engine (4) |
| Comentários no código | Referências cruzadas a nomes de arquivo antigos (por exemplo o MariaDB citando o MySQL), só nos trechos que o passo move. Comentários continuam em pt-BR. | junto com cada passo |

- `scripts/check` e hooks: nenhuma mudança (`./...` e `gofmt -l .` já cobrem
  subpacotes).

## Testing Decisions

- **Um bom teste aqui é o que já existe.** A migração é mecânica, então a
  prova de que nada mudou é a suíte atual passando antes e depois de cada
  passo: testes unitários (`./scripts/check fast`) e testes com Docker
  (`./scripts/check full`). Nenhum teste de comportamento novo é necessário, e
  nenhum teste existente pode ter sua asserção alterada. Só muda o pacote
  onde ele mora e como referencia os nomes.
- **Pontos de teste (os que já existem, reposicionados):**
  1. Suíte de conformidade genérica sobre `Engines()`, na raiz, agora
     alimentada pelo registro de fixtures do pacote `conformance`. Continua
     reprovando uma engine registrada sem fixture.
  2. Testes do conjunto montado na raiz: `InspectDump` contra engines reais
     sobre `testdata/headers/` e, quando existir, o teste de empate de
     detecção.
  3. `depguard` no `check fast` como verificação de estrutura. Ao ligá-lo,
     confirmar uma vez que ele reprova uma importação proibida feita de
     propósito, e depois reverter.
- **Onde cada teste mora:**
  - Testes unitários de uma engine vão para o pacote da engine (continuam
    enxergando o que é privado dela).
  - Testes de detecção com engine falsa (`fakeEngine`) vão para `detect`.
  - Testes do seletor e da TUI vão para `ui`.
  - Testes do `DockerHost` vão para `docker`; os da heurística vão para
    `relational`.
  - Testes que precisam de todas as engines reais ficam na raiz.
  - O teste de fluxo completo do Postgres e o de `--keep` (hoje com tag
    `docker`) vão para o pacote do Postgres, usando os helpers de
    `conformance`.
- **`testdata/headers/`** continua na raiz, um único diretório. Testes em
  pacotes internos chegam lá por um helper de caminho, não por `../../../`
  repetido.
- **Referências no código atual:** a suíte genérica sobre `Engines()` com
  fixtures registradas por engine; `fakeEngine` na detecção como exemplo de
  teste pelo contrato; os testes de caracterização de `InspectDump`.

## Out of Scope

- **Achado 10** do mapa de dívida (Docker só via `DockerHost`, injeção do host
  no `Provision`, fim do `exec` cru nas engines). Vem depois, já sobre a
  estrutura nova.
- **Achados 1, 2, 8 e 9** do mapa de dívida e qualquer outra mudança de
  comportamento.
- Módulos Go separados (um `go.mod` por engine) e pacote `internal/app`.
- Mover o `main` para `cmd/`.
- Resolver a questão aberta das citações ao `SPEC.md` inexistente
  (`.scratch/multi-engine-backup-verification/`). Comentários que o citam são
  movidos sem mudar a citação: a decisão é do mantenedor.
- Espalhar `testdata/` por pacote.

## Further Notes

- **Pré-requisito:** ticket 06 de `.scratch/core-debt-fase1/` fechado. O
  Postgres é a última engine migrada justamente para não disputar o mesmo
  código.
- **Ordem de registro:** hoje ela sai da ordem alfabética dos arquivos
  (achado 5). A lista explícita preserva a ordem atual; mudar a ordem é
  decisão à parte, porque muda o desempate de detecção.
- **SOLID/escopo:** o `CLAUDE.md` pede refatoração do repositório inteiro só
  sob pedido explícito. Este spec é esse pedido, restrito a mover e expor, sem
  redesenhar interfaces.
