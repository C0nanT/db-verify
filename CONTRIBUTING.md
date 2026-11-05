# Contribuindo

## Ambiente

```bash
git clone https://github.com/C0nanT/db-verify.git
cd db-verify
go build -o db-verify .
./scripts/install-hooks
```

`install-hooks` aponta `core.hooksPath` para `.githooks/` (só config local, sem
`git config --global`).

## Arquitetura

Um binário, um `go.mod`; o código mora em pacotes Go sob `internal/` e `engines/`. A
regra `depguard` (`.golangci.yml`) reprova importações na direção errada. As decisões
estão em [docs/adr/0001-monolito-modular.md](docs/adr/0001-monolito-modular.md), e o
mapa dos pacotes está em [CLAUDE.md](CLAUDE.md).

## Qualidade

Um único ponto de entrada: `scripts/check <nível>`.

| nível | o que roda |
|---|---|
| `fast` | `gofmt` → `go vet` → `golangci-lint` → `gitleaks` (staged) → `go test ./...` |
| `full` | tudo do `fast`, depois preflight do daemon Docker e `go test -tags docker ./...` |

`pre-commit` chama `scripts/check fast`; `pre-push` chama `scripts/check full`. O gate
de push **exige daemon Docker ligado**: se o daemon estiver parado, o push aborta.

`--no-verify` continua existindo como válvula consciente de emergência. Não há pipeline
no GitHub cobrindo o bypass: um commit/push com `--no-verify` não é revalidado fora da
máquina.

## Como adicionar uma engine nova

1. Criar um pacote `engines/<engine>/` implementando `Engine` e `Session`
   (`internal/engine`) — `Detect`, `Expects` e `Provision` do lado de `Engine`;
   `Health`, `Collections`, `Recent`, `Query`, `ConnectHint`, `Restore`, `Close` do
   lado de `Session`. Uma engine não importa outra engine, `internal/ui` nem
   `internal/detect` (o `depguard` reprova no `scripts/check fast`); código comum vai
   para `internal/relational`, `internal/docker` ou `internal/dumpio`.
2. Se a engine for relacional, reusar a heurística de `internal/relational`
   (`OrderColumnTiers`, `ChooseOrderColumn`, `OrderHint`) em vez de reimplementar a ordem de preferência
   de coluna.
3. Criar `engines/<engine>/conformance.go` com a build tag `docker`, cadastrando uma
   `conformance.ConformanceFixture` (`conformance.Register`) que descreve como gerar
   um backup mínimo válido e um truncado/corrompido. A engine só está pronta quando
   passa na suíte de conformidade genérica (`go test -tags docker ./...`) sem nenhuma
   exceção específica de engine; engine na lista sem fixture reprova a suíte.
4. Adicionar a engine à lista explícita em `engines.go` (raiz). A ordem da lista
   decide o desempate de detecção (mesma confiança): acrescente no fim, a menos que o
   desempate desejado exija outra posição.
