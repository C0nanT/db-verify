# db-verify (Go + TUI)

Sobe um PostgreSQL em Docker, restaura um `.dump` e abre uma interface no terminal
para conferir se o backup está saudável — com listagem de tabelas e os **20 registros
mais recentes** da tabela selecionada.

> Hoje só suporta PostgreSQL, mas o nome já prevê suporte a outros bancos no futuro.

## Build

```bash
cd db-verify
go build -o db-verify .
```

## Uso

Sem argumentos, o programa procura arquivos `.dump`/`.dump.gz` na pasta
`data/` ao lado do binário e abre um seletor interativo:

```bash
./db-verify
```

Também é possível apontar um arquivo diretamente, como antes:

```bash
./db-verify ../meu_backup.dump
./db-verify --keep --pg 15 --port 5555 ../meu_backup.dump
```

| flag | padrão | efeito |
|---|---|---|
| `--pg` | versão lida do dump (fallback 16) | versão da imagem `postgres:<v>-alpine` |
| `--port` | primeira livre a partir de 55432 | porta publicada em `127.0.0.1` |
| `--jobs` | 4 | paralelismo do `pg_restore` |
| `--db` | `verify` | nome do banco de destino |
| `--keep` | off | não remove o container ao sair |
| `--no-counts` | off | usa `n_live_tup` em vez de `count(*)` (bancos grandes) |

## Teclas

| tecla | ação |
|---|---|
| `↑`/`↓` ou `k`/`j` | navega pelas tabelas (consulta automática) |
| clique / roda do mouse | seleciona a tabela no painel esquerdo |
| `enter` / `r` | reexecuta a consulta |
| `←`/`→` ou `h`/`l` | rola as colunas do resultado |
| `/` | filtra tabelas (esc limpa) |
| `q` / `esc` / `ctrl+c` | sai e derruba o container |

## Como decide os "20 mais recentes"

Para cada tabela escolhe a coluna de ordenação nesta ordem: nomes conhecidos de
criação (`created_at`, `data_criacao`, …) → publicação (`data`, `published_at`, …) →
atualização (`updated_at`, …) → qualquer `timestamp`/`date` → PK de coluna única.
Sem nenhuma opção, cai em `SELECT * … LIMIT 20`. O SQL usado fica visível no painel.

## Estrutura

| arquivo | responsabilidade |
|---|---|
| `main.go` | flags, orquestração, saída da fase de restore |
| `dump.go` | detecção de formato (custom/tar/plain, gzip), versão e banco de origem |
| `docker.go` | ciclo de vida do container, cópia do dump, `pg_restore`/`psql` |
| `db.go` | consultas de saúde, listagem de tabelas e execução genérica de SQL |
| `tui.go` | interface bubbletea/lipgloss |

O container é sempre removido ao sair (inclusive em `ctrl+c`), a menos que use `--keep`.
Ao encerrar, a string de conexão é impressa para reuso com `psql`/DBeaver.
