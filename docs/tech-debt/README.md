# Índice de dívida técnica

Um módulo por revisão (skill `/tech-debt-map`). Os relatórios ficam em
`.scratch/tech-debt-map/<módulo>/<data>.md`, que é material de trabalho. Este
índice é versionado: registra quando cada módulo foi olhado pela última vez.

Os módulos abaixo correspondem aos pacotes Go do projeto (ver
`docs/adr/0001-monolito-modular.md`) e funcionam como contrato: as próximas
revisões medem cada módulo contra estes globs. Quando um corte se mostrar
errado, renomeie ou redivida o módulo aqui, sem traçar uma linha nova em
silêncio.

| Módulo | Paths | Origem | Última revisão | Relatório | Abertos |
| ------ | ----- | ------ | -------------- | --------- | ------- |
| core | `internal/engine/`, `internal/relational/`, `internal/docker/`, `internal/conformance/`, `conformance_test.go`, `engines.go` | pacotes | 2026-09-29 | `.scratch/tech-debt-map/core/2026-09-29.md` | 0 |
| detection | `internal/detect/`, `internal/dumpio/`, `detect_test.go`, `dump_test.go`, `testdata/**` | pacotes | — | — | — |
| cli-ui | `main.go`, `main_test.go`, `port_flag_test.go`, `internal/ui/` | pacotes | — | — | — |
| postgres | `engines/postgres/` | pacotes | — | — | — |
| mysql-family | `engines/mysql/` | pacotes | — | — | — |
| sqlite | `engines/sqlite/` | pacotes | — | — | — |
| redis | `engines/redis/` | pacotes | — | — | — |
| mongo | `engines/mongo/` | pacotes | — | — | — |

## Guardrails

Práticas e checagens que impedem um achado de voltar. Pertencem ao projeto,
não a um módulo. A seção só cresce: acrescente, não reescreva.

- **Provision que falha não deixa container para trás.** Essa regra faz parte
  do contrato `Engine` e deve ter subteste na suíte de conformidade (ctx
  cancelado → `!containerExists`). Toda engine nova herda a regra sem exceção.
  _(core, 2026-09-29)_
- **Empate de detecção é bug.** Um teste sobre `testdata/headers/` deve falhar
  quando duas engines devolvem a mesma confiança máxima, e `Register` deve
  rejeitar `Name()` duplicado. A ordem dos arquivos não decide nada.
  _(core, 2026-09-29)_
- **CLI Docker só via `DockerHost`.** Depois da migração, código de engine
  novo não chama `exec.Command("docker", …)` direto. Candidato a checagem por
  grep em `scripts/check fast`; como o acesso ao Docker agora mora em
  `internal/docker`, a checagem pode ser feita por pacote (grep em `engines/**` e
  `internal/ui/**`, ou uma regra `depguard` sobre `os/exec`). _(core, 2026-09-29)_
- **Regras compartilhadas entre engines moram no core com nome.** Heurística
  de ordenação, limite de "recentes" e janelas de porta ficam em constantes e
  funções de `internal/engine`/`internal/relational`, não como literais repetidos por
  engine. _(core, 2026-09-29)_
- **`os/exec` proibido no código de produção das engines.** A regra `depguard`
  `engines-sem-exec` (`.golangci.yml`, rodada por `scripts/check fast`) aplica
  o guardrail "CLI Docker só via `DockerHost`": cada engine recebe o host em
  `Engine.Host` (nil = produção). Testes e fixtures de conformidade
  (`conformance.go`, `*_conformance.go`) ficam fora da regra. _(core, 2026-10-01)_
