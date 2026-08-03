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

# Full conformance + integration suite — requires Docker, spins up real containers per
# engine (postgres, mysql, mariadb, redis, mongo images) plus in-process sqlite
go test -tags docker ./...
```

There is no separate lint config in this repo; rely on `go vet`/`gofmt` conventions.

## Architecture

### Engine/Session seam (see `engine.go`)

Everything funnels through one interface pair:

- `Engine` — recognizes a backup format (`Detect`) and provisions it (`Provision`: spin
  up container, wait ready, copy backup in, restore, connect). Deliberately fat on
  purpose, so there's exactly one seam in the project rather than many.
- `Session` — the live connection to a restored backup: `Health`, `Collections`,
  `Recent`, `Query`, `ConnectHint`, `Restore`, `Close`.

Each engine lives in its own file (`postgres.go`, `mysql.go`, `mariadb.go`, `sqlite.go`,
`redis.go`, `mongo.go`) implementing both interfaces against Docker + that engine's
driver/CLI, and self-registers via `func init() { Register(xEngine{}) }`. Callers
(`main.go`, `picker.go`, `tui.go`, `detect.go`) only ever depend on `Engine`/`Session`
and the `Engines()`/`Lookup()` registry — never on a concrete engine type. `relational.go`
holds the "choose an order column" heuristic shared by the relational engines (Postgres,
MySQL/MariaDB): a tiered list of known column names (created/published/updated/date/PK),
consumed differently by each engine's dialect but sourced from one place.

See the CLAUDE.md **SOLID** section below for how this seam is meant to be extended
(new engine = new file behind the interface, not a branch in existing code).

### Detection flow (`detect.go`)

Two phases: (1) open the file and decompress just the header (~8 KB, gzip/zstd/bzip2 —
never the whole file, so a huge dump doesn't stall detection), then (2) ask every
registered engine to `Detect` that header and keep the highest-confidence match (magic
bytes `100` > extension `50` > guess `10`; ties go to whichever engine registered
first). `--engine` skips phase 2 entirely and asks only the forced engine.

### Flow through `main.go`

Parses flags → detects/looks up the engine → `Provision` (container up, restore,
connect) → hands the resulting `Session` to the bubbletea `tui.go` model → on exit,
`Close()`s the session and prints a `ConnectHint` (DSN/shell command) for manual
follow-up, unless `--keep` was passed.

### Testing tiers

- Plain `*_test.go` (no build tag): detection, per-engine heuristics, anything that
  doesn't require a live container. Runs anywhere, no Docker needed.
- `//go:build docker` files (`conformance_test.go`, `docker_test.go`,
  `*_conformance_test.go`): require Docker. `conformance_test.go` is a single generic
  test body parameterized over `Engines()` — no branching per engine name. Each engine
  registers a `ConformanceFixture` (via `registerConformanceFixture` in its own
  `init()`) describing how to build a minimal valid backup and a truncated/corrupt one;
  "the engine is done" means it passes this suite with no engine-specific exception.
  `sqlite_test.go` covers the SQLite engine's full contract without the `docker` tag
  since it needs no container — its conformance fixture stays behind the tag only
  because the shared suite requires Docker for the other engines.
- Detection fixtures (raw headers/samples for each format) live in `testdata/headers/`.

## SOLID

Apply SOLID at the **architecture** level — module boundaries, dependency direction, and the interfaces between them. It is a way to shape seams, not a naming ritual. "Module" means whatever this codebase groups behaviour into: a class, a package, a file of functions, a service.

### Scope — boy scout rule

SOLID applies to:

- code written new in the current change, and
- the existing code the current flow already passes through, when a small local edit clears friction that change is hitting.

The rest of the codebase stays as it is. Keep a change's blast radius on the flow being built or fixed — a repo-wide SOLID refactor is its own piece of work, and happens only when explicitly asked for. The codebase converges one change at a time.

When applying a principle would require reshaping modules outside the current flow, leave them alone and say so in the summary of the change.

### In this repo

- **Policy** — `engine.go` (the `Engine`/`Session` interfaces, `Match`/`Backup`/`Collection`/`Health` types, the engine registry) and `relational.go` (heuristics shared by relational engines).
- **Details** — the per-engine files (`postgres.go`, `mysql.go`, `mariadb.go`, `mongo.go`, `redis.go`, `sqlite.go`), each implementing `Engine`/`Session` against Docker and a specific DB driver/CLI.
- **Wiring** — each engine file self-registers via `func init() { Register(xEngine{}) }`; callers (`main.go`, `picker.go`, `tui.go`, `detect.go`) depend only on the `Engine`/`Session` interfaces from `engine.go` and the `Engines()`/`Lookup()` registry, never on concrete engine types.
- **Test substitution** — tests implement `Engine`/`Session` with fakes (e.g. `fakeEngine` in `detect_test.go`) instead of standing up a real container.

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
