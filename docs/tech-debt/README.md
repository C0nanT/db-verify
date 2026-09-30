# Índice de dívida técnica

Um módulo por revisão (skill `/tech-debt-map`). Os relatórios ficam em
`.scratch/tech-debt-map/<módulo>/<data>.md`, que é material de trabalho. Este
índice é versionado: registra quando cada módulo foi olhado pela última vez.

O projeto é um único pacote `main`, sem módulos declarados. A partição abaixo
é **proposta** e funciona como contrato: as próximas revisões medem cada
módulo contra estes globs. Quando um corte se mostrar errado, renomeie ou
redivida o módulo aqui, sem traçar uma linha nova em silêncio.

| Módulo | Paths | Origem | Última revisão | Relatório | Abertos |
| ------ | ----- | ------ | -------------- | --------- | ------- |
| core | `engine.go`, `relational.go`, `internal/docker/dockerhost.go`, `internal/docker/dockerhost_test.go`, `conformance_test.go` | proposed | 2026-09-29 | `.scratch/tech-debt-map/core/2026-09-29.md` | 9 |
| detection | `detect.go`, `detect_test.go`, `dump_test.go`, `testdata/**` | proposed | — | — | — |
| cli-ui | `main.go`, `picker.go`, `picker_test.go`, `tui.go`, `tui_test.go`, `port_flag_test.go` | proposed | — | — | — |
| postgres | `postgres.go`, `postgres_conformance_test.go`, `db_test.go`, `docker_test.go` | proposed | — | — | — |
| mysql-family | `engines/mysql/` | proposed | — | — | — |
| sqlite | `engines/sqlite/` | proposed | — | — | — |
| redis | `engines/redis/` | proposed | — | — | — |
| mongo | `engines/mongo/` | proposed | — | — | — |

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
  grep em `scripts/check fast`. _(core, 2026-09-29)_
- **Regras compartilhadas entre engines moram no core com nome.** Heurística
  de ordenação, limite de "recentes" e janelas de porta ficam em constantes e
  funções de `engine.go`/`relational.go`, não como literais repetidos por
  engine. _(core, 2026-09-29)_
