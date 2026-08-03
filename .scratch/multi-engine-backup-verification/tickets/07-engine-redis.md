# 07 — Engine Redis

**What to build:** verificar um `dump.rdb` com o mesmo fluxo dos bancos relacionais.
Esta é a engine que mais tensiona o modelo genérico — se `Collection` e `ResultSet`
sobrevivem ao Redis, sobrevivem a qualquer coisa. Por isso ela vem antes das engines
fáceis: descobrir um problema de design aqui é mais barato do que descobrir depois.

Duas coisas não existem no Redis e precisam de tradução. Não há comando de restore: o
`.rdb` é colocado no diretório de dados do container **antes** do servidor subir, e um RDB
inválido faz o servidor não ficar pronto — esse caso precisa virar erro de restore com o
log do container, não um timeout genérico e confuso. E não existe "mais recente": o painel
direito mostra uma amostra de chaves do grupo, com tipo, TTL e preview do valor.

As "coleções" são grupos de chaves inferidos por prefixo até o primeiro separador
(`user:*`, `session:*`), obtidos por `SCAN` amostrado com limite — listar um keyspace de
milhões de chaves não é opção.

**Blocked by:** 03 — Detecção por registry, flags e picker multi-formato; 04 — Suíte de
conformidade que toda engine precisa passar.

**Status:** ready-for-human

- [x] Um `.rdb` é detectado pelo magic `REDIS` e pela versão do formato, também comprimido
- [x] A versão da imagem é escolhida a partir da versão do RDB, com fallback documentado
- [x] O `.rdb` é posicionado no datadir antes de o servidor subir, e os dados aparecem
      depois que ele fica pronto
- [x] Um `.rdb` inválido produz erro de restore com o log do container, e não um timeout
      genérico
- [x] Saúde mostra total de chaves, quebra por tipo (string, hash, list, set, zset,
      stream), memória usada e quantas chaves têm TTL
- [x] Campos de saúde que não se aplicam ao Redis não aparecem zerados no cabeçalho
- [x] Coleções são grupos de chaves por prefixo até o primeiro separador, com contagem
- [x] A varredura do keyspace é amostrada e limitada, e um keyspace grande não trava a TUI
- [x] Chaves sem separador caem num grupo próprio identificável em vez de sumirem
- [x] O painel direito mostra uma amostra de chaves do grupo com tipo, TTL e preview do valor
- [x] O comando Redis nativo executado aparece na tela, copiável para o `redis-cli`
- [x] O filtro `/` funciona sobre os grupos de chaves
- [x] `--keep` imprime o comando `redis-cli` para reconectar
- [x] A engine passa a suíte de conformidade sem nenhuma exceção específica
