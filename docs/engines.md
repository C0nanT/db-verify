# Detalhes das engines

## Detecção

A engine é detectada pelo conteúdo do arquivo (magic bytes), com a extensão como
desempate quando o conteúdo é ambíguo. A detecção enxerga através de gzip, zstd e
bzip2: só o cabeçalho é descomprimido antes de identificar a engine. `--engine` força
uma engine específica e pula a detecção inteiramente.

| engine | o que é reconhecido |
|---|---|
| `postgres` | magic `PGDMP` (custom), cabeçalho `PostgreSQL database dump` (plain), ou extensão `.tar`/`.sql` |
| `mysql` | cabeçalho `-- MySQL dump`, ou extensão `.sql` |
| `mariadb` | cabeçalho `-- MariaDB dump` (sem fallback de extensão — `.sql` sem esse cabeçalho vai para o MySQL) |
| `sqlite` | magic `SQLite format 3\0`, ou extensão `.sqlite`/`.db` |
| `redis` | magic `REDIS` + versão do formato do RDB, ou extensão `.rdb` |
| `mongodb` | magic number `0x8199e26d`, ou extensão `.archive` |

## Versão da imagem

A versão é resolvida nesta ordem de precedência para todas as engines: `--version-tag`
explícito → versão extraída do próprio backup (quando o formato carrega essa
informação) → fallback da tabela abaixo. `--pg` continua funcionando como alias
depreciado de `--version-tag`.

| engine | imagem | fallback de versão |
|---|---|---|
| `postgres` | `postgres:<v>-alpine` | `16` |
| `mysql` | `mysql:<v>` | `8.0` |
| `mariadb` | `mariadb:<v>` | `10.11` |
| `sqlite` | *(dispensa Docker)* | — |
| `redis` | `redis:<v>-alpine` | `7.4` |
| `mongodb` | `mongo:<v>` | `7.0` |

## SQLite sem Docker

Por ser um banco embarcado, o "restore" é só copiar o arquivo para um temporário (o
original nunca é tocado) e rodar um `PRAGMA integrity_check` nele, via driver
in-process (`modernc.org/sqlite`, puro Go, sem cgo), sem subir servidor nenhum.

## Redis

O Redis é a engine que mais foge do formato "arquivo de dump para restaurar": não há
comando de restore, o próprio `.rdb` precisa estar no datadir do container **antes** do
`redis-server` iniciar (ele só é lido na inicialização), e "coleção" é inferido por
convenção de prefixo no nome da chave (`grupo:123`), não uma entidade que o servidor
conheça.

## Como decide os "20 mais recentes"

Cada engine relacional (Postgres, MySQL, MariaDB, SQLite) escolhe a coluna de
ordenação nesta ordem: nomes conhecidos de criação (`created_at`, `data_criacao`, …)
→ publicação (`data`, `published_at`, …) → atualização (`updated_at`, …) → qualquer
`timestamp`/`date` → PK de coluna única. Sem nenhuma opção, cai em
`SELECT * … LIMIT 20`. O SQL usado fica visível no painel.

No MongoDB a lógica é a mesma família de nomes, procurados nos campos do tipo Date de
uma amostra de documentos da coleção; sem nenhum campo de data, cai em ordenar por
`_id` decrescente (que no Mongo já é cronológico por construção), então nunca fica
sem critério de ordenação. O comando `mongosh` equivalente também fica visível no
painel.

O Redis é o caso à parte: não existe conceito de "mais recente" no protocolo. A
amostra do grupo de chaves é ordenada por nome da chave em ordem decrescente antes de
aplicar o limite de 20 — determinístico, e alinhado com a convenção mais comum de
sufixar chaves com algo crescente (id, timestamp), então a chave "maior" tende a ser
a mais nova.
