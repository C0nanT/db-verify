# 12: Engine PostgreSQL no seu pacote

> **Difficulty:** Standard: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** a engine PostgreSQL passa a morar no seu próprio pacote em `engines/`, junto com o teste de fluxo completo e o de `--keep`, que hoje estão na raiz.

Cada engine que sai da raiz leva junto os seus testes unitários (que continuam enxergando o que é privado dela), transforma sua fixture de conformidade num arquivo do próprio pacote compilado só com a tag `docker` e registrado em `conformance`, e passa a aparecer na lista explícita do `main` pelo pacote novo, **na mesma posição**. As descrições do `.gitleaks.toml` e os comentários que citam os nomes de arquivo antigos dessa engine são atualizados. Citações ao `SPEC.md` inexistente ficam como estão. Nenhum comportamento muda.

Spec: `.scratch/monolito-modular/SPEC.md` (seções "Pacotes", "Suíte de conformidade" e "Documentação").

**Blocked by:** 02, 03, 04, 05b, 07

**Status:** ready-for-human

- [x] O pacote da engine importa só `engine`, `relational`, `docker` e (só nos arquivos com tag `docker`) `conformance` dentre os pacotes do projeto.
- [x] Nenhum arquivo dessa engine sobra na raiz.
- [x] A fixture de conformidade mora no pacote da engine, com tag `docker`, e a suíte genérica na raiz passa para essa engine.
- [x] A lista explícita do `main` mantém a ordem; `--list-engines` não muda.
- [x] O teste de fluxo completo do Postgres e o de `--keep` (tag `docker`) moram no pacote do Postgres e usam os helpers de `conformance`.
- [x] Nenhuma asserção de teste existente foi alterada.
- [x] `./scripts/check full` passa.
