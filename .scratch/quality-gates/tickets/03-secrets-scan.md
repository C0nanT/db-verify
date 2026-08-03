# 03 — Secrets scan com allowlist versionada

**What to build:** o contribuidor deixa de conseguir commitar uma credencial por acidente.
O gate rápido passa a escanear o conteúdo que está indo para o commit e falha se encontrar
senha, token ou DSN com credencial real. As senhas de container efêmero já hardcoded nas
engines e nos testes não geram falso positivo, graças a uma allowlist versionada e comentada
— que vira parte do contrato: engine nova que hardcodar credencial de container exige uma
entrada consciente na allowlist. Dumps reais em `data/` e fixtures em `testdata/` ficam fora
do escopo do scan.

**Blocked by:** 02 — Ferramentas pinadas e `golangci-lint` no gate rápido

**Status:** ready-for-agent

- [ ] Ferramenta de scan pinada no mesmo mecanismo do ticket 02
- [ ] `scripts/check fast` roda o scan depois do linter e antes dos testes
- [ ] Segredo plantado num arquivo a ser commitado → exit ≠ 0, etapa identificada como secrets
- [ ] Configuração de allowlist versionada, com comentário explicando cada entrada
- [ ] Credenciais efêmeras de container nas engines e testes não disparam achado
- [ ] `data/` e `testdata/` excluídos do scan
- [ ] `scripts/check fast` verde na árvore atual do repo
