# 05: Pacote `detect` dependendo só do contrato

> **Difficulty:** Standard: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** a detecção de formato (abrir o arquivo, descomprimir só o cabeçalho, pedir a cada engine registrada que o reconheça, escolher a de maior confiança, `--engine` forçado) passa a morar num pacote que conhece só o contrato `engine`. Os testes com engine falsa vão junto. Os testes que precisam das engines **reais** (caracterização de `InspectDump` sobre backups de exemplo) ficam na raiz, ao lado da lista de engines. Nenhum comportamento muda: mesmas mensagens de erro, mesmo desempate.

Spec: `.scratch/monolito-modular/SPEC.md` (seções "Pacotes" e "Testing Decisions", onde cada teste mora).

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] O pacote `detect` importa só `engine` dentre os pacotes do projeto.
- [ ] Os testes com engine falsa moram no pacote `detect`.
- [ ] Os testes de detecção com engines reais continuam na raiz e passam sem asserção alterada.
- [ ] `testdata/headers/` continua um único diretório na raiz.
- [ ] `./scripts/check full` passa.
