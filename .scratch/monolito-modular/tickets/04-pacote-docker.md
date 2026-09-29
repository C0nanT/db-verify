# 04: Pacote `docker` com o `DockerHost`

> **Difficulty:** Light: **suggested model:** Sonnet (Claude Code) / Sonnet (Cursor). Sugestão apenas, use o modelo que tiver à mão.

**What to build:** o `DockerHost` (checagem do daemon, portas livres, retry por conflito de porta, oferta de liberar porta) passa a morar num pacote próprio. As funções de pacote presas ao `DockerHost` global continuam existindo, agora exportadas, porque as engines ainda as usam. Injetar o host no `Provision` é o achado 10 do mapa de dívida e **não** faz parte deste ticket. Nenhum comportamento muda.

Spec: `.scratch/monolito-modular/SPEC.md` (seção "Pacotes", nota sobre helpers globais do Docker).

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] O pacote `docker` não importa nenhuma engine, `detect` nem `ui`.
- [ ] Os testes do `DockerHost` moram no pacote `docker`, sem asserção alterada.
- [ ] As funções globais exportadas têm comentário em pt-BR apontando para o achado 10 como o motivo de ainda existirem.
- [ ] `./scripts/check full` passa.
