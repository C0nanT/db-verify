# 11: Engines MySQL e MariaDB no mesmo pacote

> **Difficulty:** Standard: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** as engines MySQL e MariaDB passam a morar **juntas** num único pacote em `engines/`. O MariaDB reaproveita container, sessão, SQL de introspecção, formatação e resolução de versão do MySQL; no mesmo pacote, isso continua privado em vez de virar API exportada só para o vizinho.

Cada engine que sai da raiz leva junto os seus testes unitários (que continuam enxergando o que é privado dela), transforma sua fixture de conformidade num arquivo do próprio pacote compilado só com a tag `docker` e registrado em `conformance`, e passa a aparecer na lista explícita do `main` pelo pacote novo, **na mesma posição**. As descrições do `.gitleaks.toml` e os comentários que citam os nomes de arquivo antigos dessa engine são atualizados. Citações ao `SPEC.md` inexistente ficam como estão. Nenhum comportamento muda.

Spec: `.scratch/monolito-modular/SPEC.md` (seções "Pacotes", "Suíte de conformidade" e "Documentação").

**Blocked by:** 02, 03, 04, 07

**Status:** ready-for-agent

- [ ] O pacote da engine importa só `engine`, `relational`, `docker` e (só nos arquivos com tag `docker`) `conformance` dentre os pacotes do projeto.
- [ ] Nenhum arquivo dessa engine sobra na raiz.
- [ ] A fixture de conformidade mora no pacote da engine, com tag `docker`, e a suíte genérica na raiz passa para essa engine.
- [ ] A lista explícita do `main` mantém a ordem; `--list-engines` não muda.
- [ ] MySQL e MariaDB continuam duas engines distintas no registro, e o código que o MariaDB reaproveita do MySQL continua privado ao pacote.
- [ ] Nenhuma asserção de teste existente foi alterada.
- [ ] `./scripts/check full` passa.
