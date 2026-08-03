# 10 — Documentação das engines suportadas

**What to build:** o README deixa de descrever uma ferramenta de Postgres e passa a
descrever um verificador multi-engine. O operador precisa conseguir responder duas
perguntas sem ler código: "meu banco é suportado?" e "como eu gero um arquivo que essa
ferramenta consiga verificar?".

**Blocked by:** 05 — Engine MySQL; 06 — Engine MariaDB; 07 — Engine Redis; 08 — Engine
SQLite; 09 — Engine MongoDB.

**Status:** ready-for-agent

- [ ] O README lista cada engine suportada com o formato de backup esperado
- [ ] Para cada engine, o comando que gera um backup verificável está documentado
- [ ] Para cada engine, a imagem usada e o fallback de versão estão documentados
- [ ] A tabela de flags cobre `--engine`, `--list-engines` e `--version-tag`, e marca
      `--pg` como alias deprecado
- [ ] Está documentado quais formatos de compressão são aceitos
- [ ] Está documentado que SQLite dispensa Docker e que as demais engines o exigem
- [ ] Está documentado como o "20 mais recentes" é traduzido em cada engine, incluindo o
      caso do Redis, que não tem ordem temporal
- [ ] A seção de estrutura do projeto reflete a organização por engine
- [ ] Está documentado como adicionar uma engine nova: implementar a interface, registrar,
      e passar a suíte de conformidade
- [ ] A frase que diz que a ferramenta só suporta PostgreSQL é removida
