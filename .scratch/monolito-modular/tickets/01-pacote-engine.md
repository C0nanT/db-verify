# 01: Pacote `engine` com o contrato, e apelidos na raiz

> **Difficulty:** Heavy: **suggested model:** Opus (Claude Code) / Opus ou o modelo de raciocínio mais forte disponível (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** o contrato das engines (`Engine`, `Session`, os tipos compartilhados como `Match`, `Backup`, `ProvisionOpts`, `Collection`, `Health`, `ResultSet`, `ConnectHint`, `RestoreResult`, e o registro `Register`/`Engines`/`Lookup`) passa a morar num pacote interno próprio. É o passo de **expansão** da migração: a raiz ganha apelidos de tipo e funções de repasse para que todo o código que ainda está no pacote `main` continue compilando **sem ser tocado**. Os apelidos saem no ticket 13. O que o código de fora do pacote precisa usar e hoje é minúsculo (por exemplo os métodos de progresso de `ProvisionOpts`) vira exportado. Nenhum comportamento muda.

Spec: `.scratch/monolito-modular/SPEC.md` (seções "Pacotes" e "Ordem da migração", passo 1).

**Blocked by:** ticket 06 de `.scratch/core-debt-fase1/` (desempate ordinal no Postgres)

**Status:** ready-for-agent

- [ ] O pacote `engine` não importa nenhum outro pacote do projeto.
- [ ] Os testes do contrato (por exemplo `Collection.Qualified` e o registro) moram no pacote `engine`.
- [ ] O código que continua na raiz compila sem editar os arquivos das engines, da detecção nem da TUI (só a camada de apelidos é nova).
- [ ] Os apelidos na raiz ficam num arquivo só, com comentário em pt-BR dizendo que são temporários e que saem no ticket 13.
- [ ] Nenhuma asserção de teste existente foi alterada.
- [ ] `./scripts/check full` passa.
