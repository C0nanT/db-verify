# db-verify

> Confira se um backup de banco de dados restaura de verdade, direto do terminal.

Backup que nunca foi restaurado é só uma suposição. O `db-verify` recebe um arquivo de
backup, detecta a engine, sobe a versão certa dela em Docker, restaura o backup e abre
uma interface no terminal com o resultado do restore, um resumo do banco e os
**20 registros mais recentes** de cada tabela ou coleção.

Funciona com PostgreSQL, MySQL, MariaDB, SQLite, Redis e MongoDB.

## Requisitos

- Go 1.26 ou mais recente, para compilar
- Docker em execução (dispensável para SQLite, que abre o arquivo direto)

## Instalação

```bash
go install github.com/C0nanT/db-verify@latest
```

O binário vai para `$(go env GOPATH)/bin`, que precisa estar no `PATH`.

## Uso rápido

```bash
db-verify caminho/para/backup.dump
```

Sem argumentos, o programa procura backups na pasta `data/` ao lado do binário e abre
um seletor interativo, mostrando a engine detectada para cada arquivo. Com `go install`,
essa pasta é `$(go env GOPATH)/bin/data`, então costuma ser mais simples passar o
caminho do backup.

```bash
db-verify                                 # escolhe um backup de data/
db-verify data/loja.sql.gz                # backups comprimidos também funcionam
db-verify --engine mysql backup_sem_ext   # força a engine, sem detecção
db-verify --keep --port 5555 backup.dump  # mantém o container ao sair
```

Na interface, o cabeçalho mostra se o restore terminou sem erros, junto com o tamanho
do banco e contagens como tabelas, índices e chaves estrangeiras (variam por engine).

Ao sair, o container é removido (a menos que use `--keep`) e a string de conexão é
impressa para reuso com o cliente nativo da engine.

## Engines suportadas

| engine | backup aceito | como gerar |
|---|---|---|
| `postgres` | dump do `pg_dump` (custom, plain ou tar) | `pg_dump -U <user> -d <db> -Fc -f backup.dump` |
| `mysql` | dump do `mysqldump` | `mysqldump -u<user> -p <db> > backup.sql` |
| `mariadb` | dump do `mariadb-dump` | `mariadb-dump -u<user> -p <db> > backup.sql` |
| `sqlite` | o próprio arquivo `.sqlite`/`.db` | não há passo de dump |
| `redis` | arquivo `.rdb` | `redis-cli SAVE` e copiar `dump.rdb` do datadir |
| `mongodb` | archive do `mongodump` | `mongodump --archive=backup.archive --db=<db>` |

Arquivos comprimidos com gzip, zstd ou bzip2 são reconhecidos em qualquer engine.
Detecção, versões de imagem e ordenação dos registros estão em
[docs/engines.md](docs/engines.md).

## Flags

| flag | padrão | efeito |
|---|---|---|
| `--engine` | detecção automática | força a engine, pulando a detecção |
| `--list-engines` | — | lista as engines e o que cada uma reconhece, e sai |
| `--version-tag` | versão lida do backup | versão da imagem da engine |
| `--port` | escolhida pela engine | porta publicada em `127.0.0.1` |
| `--db` | `verify` | nome do banco de destino |
| `--jobs` | 4 | paralelismo do restore, quando a engine suportar |
| `--keep` | off | não remove o container ao sair |
| `--no-counts` | off | usa contagem estimada em vez de `count(*)` (bancos grandes) |

## Teclas

| tecla | ação |
|---|---|
| `↑`/`↓` ou `k`/`j` | navega pelas coleções |
| clique / roda do mouse | seleciona a coleção no painel esquerdo |
| `enter` / `r` | reexecuta a consulta |
| `←`/`→` ou `h`/`l` | rola as colunas do resultado |
| `shift+←`/`shift+→` ou `H`/`L` | rola o resultado uma tela inteira |
| `0` / `$` | vai ao início / fim da linha do resultado |
| `e` | expande as colunas (sem truncar em 28 caracteres) |
| `tab` | oculta/mostra o painel de coleções |
| `/` | filtra coleções (`esc` limpa) |
| `q` / `esc` / `ctrl+c` | sai e derruba o container |

## Limitações

- O `db-verify` mostra que o backup restaura e permite inspecionar os dados. Ele não
  compara o conteúdo com o banco de origem.
- Redis: aceita apenas RDB. "Coleções" são inferidas pelo prefixo das chaves
  (`grupo:123`), e "mais recentes" é uma aproximação pela ordem dos nomes.
- MongoDB: aceita apenas o formato `mongodump --archive`.

## Contribuindo

Veja [CONTRIBUTING.md](CONTRIBUTING.md).

## Licença

[MIT](LICENSE)
