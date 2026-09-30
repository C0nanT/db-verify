# 07: Pacote `conformance` com registro de fixtures e helpers da suíte

> **Difficulty:** Heavy: **suggested model:** Opus (Claude Code) / Opus ou o modelo de raciocínio mais forte disponível (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** a base para a suíte de conformidade sobreviver à divisão em pacotes. Um arquivo `_test.go` de um pacote não é compilado quando outro pacote o importa, então as fixtures não podem continuar em arquivos de teste. Este ticket cria um pacote `conformance`, compilado só com a tag `docker`, com:

- o tipo `ConformanceFixture` e um registro onde cada engine cadastra a sua;
- os helpers da suíte (exigir Docker, checar se container existe, nome único, achar coleção pelo nome);
- um helper que resolve o caminho de `testdata/headers/` a partir de qualquer pacote (fora da tag `docker`, porque testes unitários também o usam).

A suíte genérica continua **um teste só, na raiz**, iterando sobre `Engines()` sem ramificação por nome, agora lendo as fixtures desse registro. Ela continua reprovando uma engine registrada sem fixture. Neste ticket as fixtures das engines ainda estão na raiz; elas só passam a se cadastrar pelo registro novo. Cada ticket de engine as move depois.

Spec: `.scratch/monolito-modular/SPEC.md` (seção "Suíte de conformidade").

**Blocked by:** 01, 02

**Status:** ready-for-human

- [x] O pacote `conformance` com o registro e os helpers só compila com a tag `docker`; o binário de produção não o carrega.
- [x] A suíte genérica na raiz lê as fixtures do registro novo e não tem nenhum `if` por nome de engine.
- [x] Remover a fixture de uma engine (teste manual, revertido) faz a suíte reprovar com a mensagem de "engine registrada sem ConformanceFixture".
- [x] Os helpers antigos da suíte na raiz deixaram de existir.
- [x] `./scripts/check full` passa.
