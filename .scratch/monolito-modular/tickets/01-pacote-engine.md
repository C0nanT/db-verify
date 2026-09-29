# 01: Pacote `engine` com o contrato, e apelidos na raiz

> **Difficulty:** Heavy: **suggested model:** Opus (Claude Code) / Opus ou o modelo de raciocínio mais forte disponível (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** o contrato das engines (`Engine`, `Session`, os tipos compartilhados como `Match`, `Backup`, `ProvisionOpts`, `Collection`, `Health`, `ResultSet`, `ConnectHint`, `RestoreResult`, e o registro `Register`/`Engines`/`Lookup`) passa a morar num pacote interno próprio. É o passo de **expansão** da migração: a raiz ganha apelidos de tipo e funções de repasse para que todo o código que ainda está no pacote `main` continue compilando **sem ser tocado**. Os apelidos saem no ticket 13. O que o código de fora do pacote precisa usar e hoje é minúsculo (por exemplo os métodos de progresso de `ProvisionOpts`) vira exportado. Nenhum comportamento muda.

Spec: `.scratch/monolito-modular/SPEC.md` (seções "Pacotes" e "Ordem da migração", passo 1).

**Blocked by:** ticket 06 de `.scratch/core-debt-fase1/` (desempate ordinal no Postgres)

**Status:** ready-for-human

- [x] O pacote `engine` não importa nenhum outro pacote do projeto.
- [x] Os testes do contrato (por exemplo `Collection.Qualified` e o registro) moram no pacote `engine`.
- [x] O código que continua na raiz compila sem editar os arquivos da detecção nem da TUI (só a camada de apelidos é nova). Nas engines, só edições mecânicas forçadas pelo Go: chamadas renomeadas para `Step`/`DiscardLog` (apelido de tipo não carrega método) e literais de `HealthField` com campos nomeados (`go vet` composites).
- [x] Os apelidos na raiz ficam num arquivo só, com comentário em pt-BR dizendo que são temporários e que saem no ticket 13.
- [x] Nenhuma asserção de teste existente foi alterada.
- [x] `./scripts/check full` passa.
