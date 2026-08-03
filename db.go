package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Health é o resumo geral do banco restaurado.
type Health struct {
	Database string
	Size     string
	Tables   int
	Views    int
	Indexes  int
	Funcs    int
	FKs      int
}

// TableInfo é uma linha da listagem de tabelas.
type TableInfo struct {
	Schema   string
	Name     string
	Rows     int64
	Size     string
	OrderCol string // coluna usada para "20 mais recentes"
	ByDate   bool
}

func (t TableInfo) Qualified() string { return t.Schema + "." + t.Name }

// RecentQuery monta o SELECT dos 20 registros mais recentes da tabela.
func (t TableInfo) RecentQuery() string {
	if t.OrderCol == "" {
		return fmt.Sprintf(`SELECT * FROM %q.%q LIMIT 20;`, t.Schema, t.Name)
	}
	return fmt.Sprintf(`SELECT * FROM %q.%q ORDER BY %q DESC LIMIT 20;`, t.Schema, t.Name, t.OrderCol)
}

// ResultSet é um resultado genérico já formatado como texto.
type ResultSet struct {
	Columns []string
	Rows    [][]string
	Query   string
	Elapsed time.Duration
}

// Connect abre um pool (a TUI dispara consultas concorrentes; uma única
// pgx.Conn não é segura para uso simultâneo).
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 4
	return pgxpool.NewWithConfig(ctx, cfg)
}

const healthSQL = `
SELECT current_database(),
  pg_size_pretty(pg_database_size(current_database())),
  (SELECT count(*) FROM pg_tables  WHERE schemaname NOT IN ('pg_catalog','information_schema')),
  (SELECT count(*) FROM pg_views   WHERE schemaname NOT IN ('pg_catalog','information_schema')),
  (SELECT count(*) FROM pg_indexes WHERE schemaname NOT IN ('pg_catalog','information_schema')),
  (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
     WHERE n.nspname NOT IN ('pg_catalog','information_schema')),
  (SELECT count(*) FROM pg_constraint WHERE contype = 'f')`

func FetchHealth(ctx context.Context, c *pgxpool.Pool) (*Health, error) {
	var h Health
	err := c.QueryRow(ctx, healthSQL).Scan(&h.Database, &h.Size, &h.Tables,
		&h.Views, &h.Indexes, &h.Funcs, &h.FKs)
	return &h, err
}

// tablesSQL lista tabelas com contagem exata (via query_to_xml) ou estimada,
// e já escolhe a melhor coluna para ordenar os "mais recentes":
// datas conhecidas primeiro, senão qualquer timestamp/date, senão a PK simples.
const tablesSQL = `
WITH cols AS (
  SELECT c.table_schema, c.table_name, c.column_name,
         CASE
           WHEN c.column_name IN ('created_at','criado_em','data_criacao','data_cadastro','date_created','inserted_at','date_joined') THEN 1
           WHEN c.column_name IN ('published_at','data_publicacao','data','date','datahora','timestamp') THEN 2
           WHEN c.column_name IN ('updated_at','atualizado_em','data_atualizacao','modified','last_modified') THEN 3
           WHEN c.data_type IN ('timestamp with time zone','timestamp without time zone','date') THEN 4
           ELSE 9
         END AS pref
  FROM information_schema.columns c
  WHERE c.table_schema NOT IN ('pg_catalog','information_schema')
),
pk AS (
  SELECT n.nspname AS table_schema, cl.relname AS table_name, a.attname AS column_name
  FROM pg_constraint con
  JOIN pg_class cl     ON cl.oid = con.conrelid
  JOIN pg_namespace n  ON n.oid = cl.relnamespace
  JOIN pg_attribute a  ON a.attrelid = cl.oid AND a.attnum = ANY (con.conkey)
  WHERE con.contype = 'p' AND array_length(con.conkey, 1) = 1
),
best AS (
  SELECT DISTINCT ON (t.table_schema, t.table_name)
         t.table_schema, t.table_name,
         COALESCE(c.column_name, p.column_name) AS order_col,
         (c.column_name IS NOT NULL) AS by_date
  FROM information_schema.tables t
  LEFT JOIN cols c ON c.table_schema = t.table_schema AND c.table_name = t.table_name AND c.pref < 9
  LEFT JOIN pk   p ON p.table_schema = t.table_schema AND p.table_name = t.table_name
  WHERE t.table_type = 'BASE TABLE'
    AND t.table_schema NOT IN ('pg_catalog','information_schema')
  ORDER BY t.table_schema, t.table_name, c.pref NULLS LAST
)
SELECT b.table_schema, b.table_name,
       CASE WHEN $1 THEN
         (xpath('/row/c/text()', query_to_xml(
            format('SELECT count(*) AS c FROM %I.%I', b.table_schema, b.table_name),
            false, true, '')))[1]::text::bigint
       ELSE
         COALESCE((SELECT s.n_live_tup FROM pg_stat_user_tables s
                   WHERE s.schemaname = b.table_schema AND s.relname = b.table_name), 0)
       END AS rows,
       pg_size_pretty(pg_total_relation_size(format('%I.%I', b.table_schema, b.table_name)::regclass)),
       COALESCE(b.order_col, ''), b.by_date
FROM best b
ORDER BY 1, 2`

func FetchTables(ctx context.Context, c *pgxpool.Pool, exactCounts bool) ([]TableInfo, error) {
	rows, err := c.Query(ctx, tablesSQL, exactCounts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TableInfo
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.Schema, &t.Name, &t.Rows, &t.Size, &t.OrderCol, &t.ByDate); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RunQuery executa SQL arbitrário e devolve tudo já como string.
func RunQuery(ctx context.Context, c *pgxpool.Pool, sql string) (*ResultSet, error) {
	start := time.Now()
	rows, err := c.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rs := &ResultSet{Query: sql}
	for _, fd := range rows.FieldDescriptions() {
		rs.Columns = append(rs.Columns, string(fd.Name))
	}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make([]string, len(vals))
		for i, v := range vals {
			row[i] = formatValue(v)
		}
		rs.Rows = append(rs.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rs.Elapsed = time.Since(start)
	return rs, nil
}

func formatValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "∅"
	case time.Time:
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 {
			return x.Format("2006-01-02")
		}
		return x.Format("2006-01-02 15:04:05")
	case []byte:
		return fmt.Sprintf("\\x%x", x)
	case string:
		return strings.Join(strings.Fields(x), " ") // achata quebras de linha
	default:
		return strings.Join(strings.Fields(fmt.Sprintf("%v", x)), " ")
	}
}
