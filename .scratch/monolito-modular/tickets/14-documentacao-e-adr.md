# 14: Documentação dos pacotes e ADR 0001

> **Difficulty:** Standard: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** a documentação passa a descrever a estrutura real, e as decisões não óbvias da migração ficam registradas com o motivo, para que ninguém as "conserte" depois.

- **`CLAUDE.md`:** as seções *Architecture* (seam Engine/Session, fluxo de detecção, fluxo do `main`), *Testing tiers* e *SOLID → In this repo* falam de pacotes, da lista explícita de engines, da fixture com build tag registrada em `conformance` e da regra do `depguard`. *Commands* não muda.
- **`README.md`** (pt-BR): a tabela de arquivos vira tabela de pacotes. O roteiro "como adicionar uma engine" vira: criar pacote em `engines/`, implementar `Engine`/`Session`, criar a fixture com tag `docker`, adicionar à lista do `main`.
- **`docs/tech-debt/README.md`:** a tabela de módulos aponta para os pacotes e deixa de ser `proposed`. O guardrail "CLI Docker só via `DockerHost`" ganha a nota de que a checagem agora pode ser feita por pacote.
- **ADR 0001 em `docs/adr/`** (pasta nova; ver `docs/agents/domain.md`): monólito modular. Decisões e motivos: pacotes em `internal/` num único `go.mod`; MySQL e MariaDB juntos; lista explícita no lugar de `init()`, preservando a ordem de desempate; fixture com build tag para manter a trava "engine sem fixture reprova"; testes do conjunto montado na raiz; `run()` no `main` sem `internal/app`; `testdata/headers/` único na raiz; `depguard` como fiscal das fronteiras; achado 10 deixado para depois.

Spec: `.scratch/monolito-modular/SPEC.md` (seção "Documentação").

**Blocked by:** 13

**Status:** ready-for-human

- [x] Nenhum dos documentos acima cita um arquivo Go da raiz que não existe mais.
- [x] O `CLAUDE.md` não menciona `func init() { Register(...) }` nem `registerConformanceFixture` como mecanismo atual.
- [x] O ADR existe, em pt-BR, com contexto, decisão e consequências.
- [x] `./scripts/check fast` passa.
