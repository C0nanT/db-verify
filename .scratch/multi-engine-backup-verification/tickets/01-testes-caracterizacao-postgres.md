# 01 — Testes de caracterização do comportamento Postgres atual

**What to build:** uma bateria de testes de funcionalidade que descreve o que o
`db-verify` faz hoje com um backup Postgres, escrita contra o código como ele está.
Nenhuma linha de produção muda neste ticket. O valor é confiança: depois disso, a
refatoração para multi-engine tem um critério objetivo de "não quebrei nada".

Só teste de funcionalidade. Sem quality gate — nada de lint obrigatório, meta de
cobertura, CI bloqueante ou pre-commit hook. Isso fica para outra spec.

Os testes de detecção rodam sem Docker, com fixtures de cabeçalho versionadas no repo
(algumas centenas de bytes de cada formato, geradas a partir de dumps reais e sem dados
sensíveis). Os testes de fluxo completo exigem Docker e ficam atrás de build tag, com
o dump de teste gerado pelo próprio teste — os arquivos em `data/` não entram no repo.

**Blocked by:** None — can start immediately.

**Status:** ready-for-human

- [x] Detecção identifica formato, versão de origem e banco de origem para: custom
      (`PGDMP`), plain SQL, e tar
- [x] Cada uma dessas fixtures também é detectada corretamente quando gzipada
- [x] Arquivo vazio e arquivo de texto aleatório produzem o comportamento atual,
      documentado pelo teste
- [x] Fluxo completo contra um dump Postgres pequeno gerado no teste: restore reporta
      zero erros
- [x] A listagem de tabelas devolve as tabelas esperadas com contagem exata correta
- [x] Uma tabela vazia aparece na listagem com contagem zero, não é omitida
- [x] A heurística de coluna de ordenação é assertada para cada família suportada hoje:
      criação (`created_at`, `data_criacao`), publicação (`published_at`, `data`),
      atualização (`updated_at`), timestamp/date genérico, PK de coluna única, e o caso
      sem nenhuma opção
- [x] O SQL resultante de cada caso acima é assertado, incluindo o fallback
      `SELECT * … LIMIT 20`
- [x] Consulta de recentes devolve no máximo 20 linhas, em ordem decrescente pela coluna
      escolhida
- [x] Formatação de valores é assertada para: nulo, date, timestamp, binário, e string
      com quebra de linha
- [x] O container é removido ao encerrar, e preservado quando `--keep` é usado —
      verificado por inspeção do Docker
- [x] Toda a suíte passa contra o código atual, sem nenhuma alteração em código de produção
