# db-verify (Go + TUI)

Sobe o banco certo em Docker (ou abre o arquivo direto, no caso do SQLite), restaura
um backup e abre uma interface no terminal para conferir se ele está saudável — com
listagem de coleções (tabelas, coleções do Mongo, grupos de chaves do Redis…) e os
**20 registros mais recentes** da coleção selecionada.

Verificador de backups **agnóstico de banco**: aponte para um arquivo de qualquer
engine suportada e o fluxo é sempre o mesmo — detecção, container, restore, TUI.

## Build

```bash
cd db-verify
go build -o db-verify .
```

## Uso

Sem argumentos, o programa procura arquivos de backup reconhecidos na pasta `data/`
ao lado do binário e abre um seletor interativo, mostrando a engine detectada para
cada um:

```bash
./db-verify
```

Também é possível apontar um arquivo diretamente:

```bash
./db-verify data/loja.sql.gz
./db-verify data/dump.rdb
./db-verify data/app.sqlite
./db-verify data/mongo-backup.archive
./db-verify --engine mysql backup_sem_extensao
./db-verify --keep --version-tag 15 --port 5555 ../meu_backup.dump
```

A engine é detectada pelo conteúdo do arquivo (magic bytes), com a extensão como
desempate quando o conteúdo é ambíguo. `--engine` força uma engine específica e pula
a detecção inteiramente; `--list-engines` lista as engines registradas e o que cada
uma reconhece.

## Engines suportadas

| engine | formato do backup | comando para gerar um backup verificável | imagem | fallback de versão |
|---|---|---|---|---|
| `postgres` | dump do `pg_dump`: magic `PGDMP` (custom), cabeçalho `PostgreSQL database dump` (plain), ou extensão `.tar`/`.sql` | `pg_dump -U <user> -d <db> -Fc -f backup.dump` | `postgres:<v>-alpine` | `16` |
| `mysql` | dump do `mysqldump`: cabeçalho `-- MySQL dump`, ou extensão `.sql` | `mysqldump -u<user> -p<senha> <db> > backup.sql` | `mysql:<v>` | `8.0` |
| `mariadb` | dump do `mariadb-dump`: cabeçalho `-- MariaDB dump` (sem fallback de extensão — `.sql` sem esse cabeçalho vai para o MySQL) | `mariadb-dump -u<user> -p<senha> <db> > backup.sql` | `mariadb:<v>` | `10.11` |
| `sqlite` | arquivo SQLite: magic `SQLite format 3\0`, ou extensão `.sqlite`/`.db` | o próprio arquivo `.sqlite`/`.db` já é o backup — não há passo de dump | *(dispensa Docker — ver abaixo)* | — |
| `redis` | dump `.rdb`: magic `REDIS` + versão do formato do RDB, ou extensão `.rdb` | `redis-cli SAVE` (ou deixar o `BGSAVE` automático rodar) e copiar `dump.rdb` do datadir | `redis:<v>-alpine` | `7.4` |
| `mongodb` | archive do `mongodump`: magic number `0x8199e26d`, ou extensão `.archive` | `mongodump --archive=backup.archive --db=<db>` | `mongo:<v>` | `7.0` |

A versão da imagem é resolvida nesta ordem de precedência para todas as engines:
`--version-tag` explícito → versão extraída do próprio backup (quando o formato
carrega essa informação) → fallback da tabela acima. `--pg` continua funcionando
como alias depreciado de `--version-tag`.

O Redis é a engine que mais foge do formato "arquivo de dump para restaurar":
não há comando de restore, o próprio `.rdb` precisa estar no datadir do container
**antes** do `redis-server` iniciar (ele só é lido na inicialização), e "coleção" é
inferido por convenção de prefixo no nome da chave (`grupo:123`), não uma entidade
que o servidor conheça.

## Flags

| flag | padrão | efeito |
|---|---|---|
| `--version-tag` | versão lida do backup (fallback por engine, ver tabela acima) | versão da imagem da engine detectada |
| `--pg` | — | **depreciado**, alias de `--version-tag` |
| `--port` | primeira livre a partir de 55432 (ou da porta padrão da engine) | porta publicada em `127.0.0.1` |
| `--jobs` | 4 | paralelismo do restore, quando a engine suportar |
| `--db` | `verify` | nome do banco de destino |
| `--keep` | off | não remove o container ao sair |
| `--no-counts` | off | usa contagem estimada em vez de `count(*)`/equivalente (bancos grandes) |
| `--engine` | detecção automática | força a engine, pulando a detecção; veja `--list-engines` |
| `--list-engines` | — | lista as engines suportadas e o que cada uma reconhece, e sai |

## Teclas

| tecla | ação |
|---|---|
| `↑`/`↓` ou `k`/`j` | navega pelas coleções (consulta automática) |
| clique / roda do mouse | seleciona a coleção no painel esquerdo |
| `enter` / `r` | reexecuta a consulta |
| `←`/`→` ou `h`/`l` | rola as colunas do resultado |
| `/` | filtra coleções (esc limpa) |
| `q` / `esc` / `ctrl+c` | sai e derruba o container |

## Compressão aceita

A detecção enxerga através de gzip, zstd e bzip2 — o cabeçalho é descomprimido antes
de identificar a engine, então `dump.sql.gz`, `backup.rdb.gz` etc. são reconhecidos
normalmente, para qualquer engine.

## Docker

Todas as engines exigem Docker, **exceto SQLite**: por ser um banco embarcado, o
"restore" é só copiar o arquivo para um temporário (o original nunca é tocado) e
rodar um `PRAGMA integrity_check` nele, via driver in-process (`modernc.org/sqlite`,
puro Go, sem cgo) — sem subir servidor nenhum. As demais engines sobem um container
da imagem/versão resolvida, restauram e conectam nele.

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

## Estrutura

| arquivo | responsabilidade |
|---|---|
| `main.go` | flags, orquestração, saída da fase de restore |
| `engine.go` | interfaces `Engine`/`Session`, tipos compartilhados (`Match`, `Backup`, `Collection`, `Health`…) e o registro de engines |
| `relational.go` | heurística de coluna de ordenação compartilhada entre as engines relacionais |
| `detect.go` | detecção de formato genérica: descompressão do cabeçalho + disputa entre engines registradas |
| `postgres.go` | engine PostgreSQL (`pg_restore`/`psql` via `pgx`) |
| `mysql.go` | engine MySQL (`mysql`/`mysqldump` via `database/sql`) |
| `mariadb.go` | engine MariaDB (espelha `mysql.go`, imagem/binários `mariadb-*`) |
| `sqlite.go` | engine SQLite (driver in-process, sem container) |
| `redis.go` | engine Redis (RDB posicionado no datadir antes do `redis-server` subir) |
| `mongo.go` | engine MongoDB (`mongorestore --archive`, achatamento de documentos aninhados) |
| `picker.go` | seletor interativo de backups em `data/`, com a engine detectada de cada um |
| `tui.go` | interface bubbletea/lipgloss |

O container é sempre removido ao sair (inclusive em `ctrl+c`), a menos que use
`--keep`. Ao encerrar, a string de conexão é impressa para reuso com o cliente
nativo da engine (`psql`, `mysql`, `redis-cli`, `mongosh`…) ou DBeaver.

## Como adicionar uma engine nova

1. Criar um arquivo `<engine>.go` implementando `Engine` e `Session` (`engine.go`) —
   `Detect`, `Expects` e `Provision` do lado de `Engine`; `Health`, `Collections`,
   `Recent`, `Query`, `ConnectHint`, `Restore`, `Close` do lado de `Session`.
2. Registrar em um `func init() { Register(xEngine{}) }` no mesmo arquivo — nenhum
   outro arquivo do projeto precisa saber que a engine nova existe.
3. Se a engine for relacional, reusar a heurística de `relational.go`
   (`orderColumnTiers`, `orderHint`) em vez de reimplementar a ordem de preferência
   de coluna.
4. Registrar uma `ConformanceFixture` (`conformance_test.go`) num
   `<engine>_conformance_test.go`, descrevendo como gerar um backup mínimo válido e
   um truncado/corrompido. A engine só está pronta quando passa na suíte de
   conformidade genérica (`go test -tags docker ./...`) sem nenhuma exceção
   específica de engine.
