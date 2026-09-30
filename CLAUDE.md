# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`db-verify` is a CLI/TUI tool that verifies database backups: given a backup file (any
supported engine), it detects the engine, spins up the right version of that engine in
Docker, restores the backup, and opens a terminal UI to inspect collections/tables and
their most recent rows, plus a health summary. Supported engines: PostgreSQL, MySQL,
MariaDB, SQLite, Redis, MongoDB. See `.scratch/multi-engine-backup-verification/SPEC.md`
for the full product spec and `.scratch/multi-engine-backup-verification/tickets/` for
the per-engine implementation tickets.

Code comments and doc-strings throughout the codebase are written in Portuguese
(pt-BR) — match that convention when editing existing files. README.md is also in
Portuguese.

## Commands

```bash
# Build
go build -o db-verify .

# Run (no args: picks a backup interactively from ./data)
./db-verify
./db-verify path/to/backup.dump
./db-verify --engine mysql path/to/backup   # force engine, skip detection
./db-verify --list-engines                  # list registered engines and what they expect

# Unit tests — no Docker required (detection, heuristics, per-engine logic that doesn't
# need a live container)
go test ./...

# Single test
go test -run TestFuncName ./...

# Quality gates — unique entry point (hooks call these; do not redeclare the command list)
./scripts/check fast    # gofmt, go vet, golangci-lint, gitleaks (staged), go test ./...
./scripts/check full    # fast + Docker daemon preflight + go test -tags docker ./...
./scripts/install-hooks # local core.hooksPath=.githooks (pre-commit=fast, pre-push=full)
```

`./scripts/check full` leva ~3 min (suíte Docker), acima do timeout padrão de 120s do
Bash. Rode com `run_in_background: true` (ou `timeout: 600000`) e espere a notificação de
conclusão. Nunca faça polling com `until ! pgrep -f "scripts/check"; do sleep …; done`:
o `-f` casa com a própria linha de comando do shell que contém a string, então o laço
nunca termina e só sai no timeout de 10 min. Se precisar esperar, use `Monitor` ou
`pgrep -x`/PID.

Static lint is `golangci-lint` (staticcheck, errcheck, ineffassign, unused, govet),
configured in `.golangci.yml` and pinned as a Go tool dependency in `go.mod`. Agents
should run it through `scripts/check fast` (after `go vet`, before unit tests), not as a
separate ad-hoc lint path. Secrets scan config is `.gitleaks.toml`.

## Architecture

One binary, one `go.mod`, but the code lives in Go packages under `internal/` (plus
`engines/`), so the compiler hides what is private to each package and the `depguard`
rule in `.golangci.yml` (run by `scripts/check fast`) rejects imports that point the
wrong way. Decisions and rationale: `docs/adr/0001-monolito-modular.md`.

| Package | Responsibility | May import (project) |
|---|---|---|
| `main` (root) | flags, `run()`, the explicit engine list (`engines.go`), tests of the assembled set | all |
| `internal/engine` | `Engine`/`Session` interfaces, shared types (`Match`, `Backup`, `ProvisionOpts`, `Collection`, `Health`, `ResultSet`…), `HumanSize`, registry (`Register`, `Engines`, `Lookup`) | none |
| `internal/relational` | order-column heuristic shared by relational engines | `engine` |
| `internal/docker` | `DockerHost`: daemon check, free ports, port-conflict retry | — |
| `internal/dumpio` | `OpenMaybeCompressed` (gzip/zstd/bzip2) | none |
| `internal/detect` | header reading, engine contest, `InspectDump` | `engine`, `dumpio` |
| `internal/ui` | backup picker and bubbletea TUI | `engine`, `detect` |
| `internal/conformance` | fixture registry and suite helpers (`docker` build tag), `HeaderPath` for `testdata/headers/` | `engine` |
| `engines/postgres`, `engines/mysql` (MySQL + MariaDB), `engines/sqlite`, `engines/redis`, `engines/mongo` | one engine each | `engine`, `relational`, `docker`, `dumpio`, `conformance` (only in `docker`-tagged files) |

### Engine/Session seam (`internal/engine`)

Everything funnels through one interface pair:

- `Engine` — recognizes a backup format (`Detect`) and provisions it (`Provision`: spin
  up container, wait ready, copy backup in, restore, connect). Deliberately fat on
  purpose, so there's exactly one seam in the project rather than many.
- `Session` — the live connection to a restored backup: `Health`, `Collections`,
  `Recent`, `Query`, `ConnectHint`, `Restore`, `Close`.

Each engine is a package implementing both interfaces against Docker + that engine's
driver/CLI. Engines do **not** self-register: `engines.go` in the root holds the single
explicit list (a package-level initializer calling `engine.Register` per engine), in
the order they are tried. Callers (`internal/ui`, `internal/detect`) only ever depend
on `Engine`/`Session` and the `Engines()`/`Lookup()` registry — never on a concrete
engine package. `internal/relational` holds the "choose an order column" heuristic
shared by the relational engines (Postgres, MySQL/MariaDB): a tiered list of known
column names (created/published/updated/date/PK), consumed differently by each
engine's dialect but sourced from one place.

See the **SOLID** section below for how this seam is meant to be extended
(new engine = new package behind the interface plus one line in the list, not a
branch in existing code).

### Detection flow (`internal/detect`)

Two phases: (1) open the file and decompress just the header (~8 KB, gzip/zstd/bzip2 via
`internal/dumpio` — never the whole file, so a huge dump doesn't stall detection), then
(2) ask every registered engine to `Detect` that header and keep the highest-confidence
match (magic bytes `100` > extension `50` > guess `10`; ties go to whichever engine
comes first in the list in `engines.go`). `--engine` skips phase 2 entirely and asks
only the forced engine.

### Flow through `main.go`

`run()` reads top to bottom: parses flags → detects/looks up the engine → `Provision`
(container up, restore, connect) → hands the resulting `Session` to the bubbletea model
in `internal/ui` → on exit, `Close()`s the session and prints a `ConnectHint`
(DSN/shell command) for manual follow-up, unless `--keep` was passed. There is no
`internal/app`: with a single entry point it would be only indirection.

### Testing tiers

- Plain `*_test.go` (no build tag): detection, per-engine heuristics, anything that
  doesn't require a live container. Runs anywhere, no Docker needed. Unit tests of an
  engine live in that engine's package; `detect` tests use fakes; tests needing all
  real engines (`InspectDump` over `testdata/headers/`, detection ties) sit in the root
  next to `engines.go`.
- `//go:build docker` files (`conformance_test.go` in the root,
  `engines/postgres/docker_test.go`, `engines/*/conformance.go`,
  `internal/conformance/`): require Docker. `conformance_test.go` is a single generic
  test body parameterized over `Engines()` — no branching per engine name. Each engine
  package has a `conformance.go` with the `docker` build tag (not a `_test.go`, so other
  packages can compile it) that registers, via `conformance.Register`, a
  `conformance.ConformanceFixture` describing how to build a minimal valid backup and a
  truncated/corrupt one. An engine in the list without a fixture fails the suite:
  "the engine is done" means it passes with no engine-specific exception.
  `engines/sqlite/sqlite_test.go` covers the SQLite engine's full contract without the
  `docker` tag since it needs no container — its conformance fixture stays behind the tag
  only because the shared suite requires Docker for the other engines.
- Detection fixtures (raw headers/samples for each format) live in the single
  `testdata/headers/` at the root; packages reach it through `conformance.HeaderPath`,
  not `../../..` paths.

## SOLID

Apply SOLID at the **architecture** level — module boundaries, dependency direction, and the interfaces between them. It is a way to shape seams, not a naming ritual. "Module" means whatever this codebase groups behaviour into: a class, a package, a file of functions, a service.

### Scope — boy scout rule

SOLID applies to:

- code written new in the current change, and
- the existing code the current flow already passes through, when a small local edit clears friction that change is hitting.

The rest of the codebase stays as it is. Keep a change's blast radius on the flow being built or fixed — a repo-wide SOLID refactor is its own piece of work, and happens only when explicitly asked for. The codebase converges one change at a time.

When applying a principle would require reshaping modules outside the current flow, leave them alone and say so in the summary of the change.

### In this repo

- **Policy** — `internal/engine` (the `Engine`/`Session` interfaces, `Match`/`Backup`/`Collection`/`Health` types, the registry) and `internal/relational` (heuristics shared by relational engines).
- **Details** — the engine packages (`engines/postgres`, `engines/mysql` (MySQL + MariaDB), `engines/mongo`, `engines/redis`, `engines/sqlite`), each implementing `Engine`/`Session` against Docker and a specific DB driver/CLI.
- **Wiring** — the explicit list in `engines.go` (root) is the only place that names concrete engines and their tie-break order; `internal/ui` and `internal/detect` depend only on `internal/engine` and the `Engines()`/`Lookup()` registry.
- **Boundary enforcement** — the `depguard` rules in `.golangci.yml` (run by `scripts/check fast`): an engine does not import another engine, `internal/ui` or `internal/detect`; `ui` and `detect` do not import any `engines/*` package; `engine` and `dumpio` import no other project package. Shared code goes to `relational`, `docker` or `dumpio`, not across engines.
- **Test substitution** — tests implement `Engine`/`Session` with fakes (e.g. `fakeEngine` in `internal/detect/detect_test.go`) instead of standing up a real container.

### The principles, as architecture rules

- **SRP** — a module has one reason to change. When one flow forces edits in a module that other flows also own for unrelated reasons, that module is holding two responsibilities.
- **OCP** — new behaviour arrives as a new implementation behind an existing interface, rather than another branch in a growing conditional over kinds of thing.
- **LSP** — every implementation of an interface is substitutable through that interface: same contract, same error behaviour, no "this one also needs X called first".
- **ISP** — a consumer depends on the narrow interface it actually uses. Interfaces are shaped by the caller's need, not by everything the implementation can do.
- **DIP** — policy does not depend on details (see *In this repo* above for both). The interface belongs to the policy side; the detail implements it and is passed in.

### Applying it

- When a new flow crosses an IO boundary, define the interface from the policy side and inject the implementation.
- One production implementation is enough **when a test substitutes it** — the test double is the second implementation, and the interface is the test surface. An adapter behind an interface with a single caller and no substitution is a hypothetical seam: drop the interface until something real needs it.

## Agent skills

### Issue tracker

Local markdown under `.scratch/<feature>/`. See `docs/agents/issue-tracker.md`.

### Domain docs

Single-context — `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.

### Git guardrails

Destructive git (`commit`, `push`, `reset`, …) is denied via `permissions.deny` in `.claude/settings.json`. See `docs/agents/git-guardrails.md`.
