package main

// Implementação da engine PostgreSQL atrás da interface Engine/Session. Toda
// a variação específica de Postgres (pgx, information_schema, "schema.tabela",
// pg_restore/psql) fica contida neste arquivo — main.go e a TUI só falam com
// Engine/Session.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func init() { Register(pgEngine{}) }

// pgEngine implementa Engine para PostgreSQL.
type pgEngine struct{}

func (pgEngine) Name() string { return "postgres" }

var (
	reHeaderVersion = regexp.MustCompile(`^(\d{1,2})\.\d+`)
	rePlainVersion  = regexp.MustCompile(`Dumped from database version (\d{1,2})`)
	reConnect       = regexp.MustCompile(`(?m)^\\connect (\S+)`)
)

// Detect reproduz a detecção de formato que o db-verify sempre fez para
// dumps do pg_dump: PGDMP binário (custom), "PostgreSQL database dump"
// (plain), ".tar" por extensão, e um palpite de "plain" para qualquer outra
// coisa — a ambiguidade conhecida e aceita (ver SPEC.md, "Ambiguidade
// conhecida e resolvida por decisão").
func (pgEngine) Detect(head []byte, path string) (Match, bool) {
	switch {
	case bytes.HasPrefix(head, []byte("PGDMP")):
		m := Match{Format: "custom", Confidence: ConfidenceMagic}
		parts := printableStrings(head[:min(512, len(head))])
		if len(parts) > 1 {
			m.OriginDB = parts[1]
		}
		for _, p := range parts {
			if mm := reHeaderVersion.FindStringSubmatch(p); mm != nil {
				m.Version = mm[1]
				break
			}
		}
		return m, true
	case bytes.Contains(head, []byte("PostgreSQL database dump")):
		m := Match{Format: "plain", Confidence: ConfidenceMagic}
		if mm := rePlainVersion.FindSubmatch(head); mm != nil {
			m.Version = string(mm[1])
		}
		if mm := reConnect.FindSubmatch(head); mm != nil {
			m.OriginDB = strings.Trim(string(mm[1]), `";`)
		}
		return m, true
	case strings.HasSuffix(strings.ToLower(path), ".tar"):
		return Match{Format: "tar", Confidence: ConfidenceExtension}, true
	default:
		return Match{Format: "plain", Confidence: ConfidenceGuess}, true // palpite: SQL puro
	}
}

// Expects descreve o que o Postgres reconhece, para mensagens de erro e
// --list-engines.
func (pgEngine) Expects() string {
	return "dumps do pg_dump: magic \"PGDMP\" (custom), cabeçalho \"PostgreSQL database dump\" (plain), ou extensão .tar/.sql"
}

// Provision sobe o container, espera ficar pronto, copia o dump, restaura e
// conecta — deliberadamente grosso, para o número de seams continuar sendo
// um. opts.Progress, se houver, é chamado a cada fase para o chamador
// imprimir o mesmo acompanhamento de sempre.
func (pgEngine) Provision(ctx context.Context, b *Backup, opts ProvisionOpts) (Session, error) {
	if err := dockerAvailable(); err != nil {
		return nil, err
	}

	version := opts.VersionTag
	if version == "" {
		version = b.Version
		if version == "" {
			version = "16"
		}
	}
	port := opts.Port
	if port == 0 {
		port = freePort()
	}

	cont := &pgContainer{
		Name:  fmt.Sprintf("db-verify-%d", os.Getpid()),
		Image: "postgres:" + version + "-alpine",
		Port:  port, DB: opts.DBName, User: "postgres", Pass: "postgres",
	}

	opts.report("subindo container %s (imagem %s)…", cont.Name, cont.Image)
	if err := cont.Start(ctx); err != nil {
		return nil, err
	}
	opts.report("aguardando o Postgres ficar pronto…")
	if err := cont.WaitReady(ctx, 90*time.Second); err != nil {
		cont.Remove()
		return nil, err
	}
	opts.report("copiando dump para o container…")
	if err := cont.CopyDump(ctx, b); err != nil {
		cont.Remove()
		return nil, err
	}
	opts.report("restaurando (pode demorar)…")
	res, err := cont.Restore(ctx, b, opts.Jobs)
	if err != nil {
		cont.Remove()
		return nil, err
	}

	pool, err := pgConnect(ctx, cont.DSN())
	if err != nil {
		cont.Remove()
		return nil, fmt.Errorf("conexão falhou: %w", err)
	}

	return &pgSession{pool: pool, cont: cont, restore: res}, nil
}

// ---------------------------------------------------------- container ---

// pgContainer representa o Postgres temporário usado para validar o backup.
type pgContainer struct {
	Name  string
	Image string
	Port  int
	DB    string
	User  string
	Pass  string
}

func (c *pgContainer) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@127.0.0.1:%d/%s?sslmode=disable",
		c.User, c.Pass, c.Port, c.DB)
}

func dockerAvailable() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker não encontrado no PATH")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		return fmt.Errorf("docker daemon não está acessível")
	}
	return nil
}

// freePort procura uma porta livre a partir de 55432.
func freePort() int {
	for p := 55432; p < 55532; p++ {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			l.Close()
			return p
		}
	}
	return 55432
}

// Start sobe o container com fsync desligado (restore mais rápido, dado descartável).
func (c *pgContainer) Start(ctx context.Context) error {
	args := []string{
		"run", "-d", "--name", c.Name,
		"-e", "POSTGRES_PASSWORD=" + c.Pass,
		"-e", "POSTGRES_USER=" + c.User,
		"-e", "POSTGRES_DB=" + c.DB,
		"-p", fmt.Sprintf("127.0.0.1:%d:5432", c.Port),
		c.Image,
		"-c", "fsync=off", "-c", "full_page_writes=off", "-c", "synchronous_commit=off",
	}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("falha ao subir container: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *pgContainer) WaitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := exec.CommandContext(ctx, "docker", "exec", c.Name,
			"pg_isready", "-U", c.User, "-d", c.DB).Run()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	logs, _ := exec.Command("docker", "logs", "--tail", "20", c.Name).CombinedOutput()
	return fmt.Errorf("timeout esperando o Postgres:\n%s", string(logs))
}

// CopyDump joga o arquivo dentro do container, descomprimindo se preciso.
func (c *pgContainer) CopyDump(ctx context.Context, b *Backup) error {
	if b.Compression == "none" {
		out, err := exec.CommandContext(ctx, "docker", "cp", b.Path, c.Name+":/tmp/backup.dump").CombinedOutput()
		if err != nil {
			return fmt.Errorf("docker cp falhou: %s", strings.TrimSpace(string(out)))
		}
		return nil
	}
	r, _, err := openMaybeCompressed(b.Path)
	if err != nil {
		return err
	}
	defer r.Close()

	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", c.Name, "sh", "-c", "cat > /tmp/backup.dump")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	_, copyErr := io.Copy(stdin, r)
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("falha ao copiar dump: %w", err)
	}
	return copyErr
}

var reRestoreErr = regexp.MustCompile(`(?im)^(pg_restore: )?(error|erro):|^ERROR:`)

func (c *pgContainer) Restore(ctx context.Context, b *Backup, jobs int) (*RestoreResult, error) {
	start := time.Now()
	var args []string
	if b.Format == "plain" {
		args = []string{"exec", c.Name, "psql", "-U", c.User, "-d", c.DB,
			"-v", "ON_ERROR_STOP=0", "-f", "/tmp/backup.dump"}
	} else {
		args = []string{"exec", c.Name, "pg_restore", "-U", c.User, "-d", c.DB,
			"--no-owner", "--no-privileges", fmt.Sprintf("--jobs=%d", jobs), "/tmp/backup.dump"}
	}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()

	res := &RestoreResult{Duration: time.Since(start)}
	if ee, ok := err.(*exec.ExitError); ok {
		res.ExitCode = ee.ExitCode()
	} else if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if reRestoreErr.MatchString(line) {
			res.Errors = append(res.Errors, strings.TrimSpace(line))
		}
	}
	if len(res.Errors) > 0 {
		if f, e := os.CreateTemp("", "db-verify-*.log"); e == nil {
			f.Write(out)
			f.Close()
			res.LogPath = f.Name()
		}
	}
	return res, nil
}

func (c *pgContainer) Remove() {
	exec.Command("docker", "rm", "-f", c.Name).Run()
}

// ------------------------------------------------------------- session ---

// pgConnect abre um pool (a TUI dispara consultas concorrentes; uma única
// pgx.Conn não é segura para uso simultâneo).
func pgConnect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
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

// tablesSQL lista tabelas com contagem exata (via query_to_xml) ou estimada,
// e já escolhe a melhor coluna para ordenar os "mais recentes": datas
// conhecidas primeiro, senão qualquer timestamp/date, senão a PK simples.
var tablesSQL = `
WITH cols AS (
  SELECT c.table_schema, c.table_name, c.column_name,
         CASE
           WHEN c.column_name IN (` + sqlStringList(orderColumnTiers[0]) + `) THEN 1
           WHEN c.column_name IN (` + sqlStringList(orderColumnTiers[1]) + `) THEN 2
           WHEN c.column_name IN (` + sqlStringList(orderColumnTiers[2]) + `) THEN 3
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

// pgDescriptor é o Descriptor opaco que Collections anexa a cada Collection e
// que Recent usa para montar o SELECT — só a engine Postgres sabe o que
// esses campos significam.
type pgDescriptor struct {
	OrderCol string
	ByDate   bool
}

// pgRecentQuery monta o SELECT dos 20 registros mais recentes da tabela.
func pgRecentQuery(namespace, name string, d pgDescriptor) string {
	if d.OrderCol == "" {
		return fmt.Sprintf(`SELECT * FROM %q.%q LIMIT 20;`, namespace, name)
	}
	return fmt.Sprintf(`SELECT * FROM %q.%q ORDER BY %q DESC LIMIT 20;`, namespace, name, d.OrderCol)
}

type pgSession struct {
	pool    *pgxpool.Pool
	cont    *pgContainer
	restore *RestoreResult
}

func (s *pgSession) Health(ctx context.Context) (*Health, error) {
	var name, size string
	var tables, views, indexes, funcs, fks int
	err := s.pool.QueryRow(ctx, healthSQL).Scan(&name, &size, &tables,
		&views, &indexes, &funcs, &fks)
	if err != nil {
		return nil, err
	}
	return &Health{
		Name: name,
		Size: size,
		Fields: []HealthField{
			{"tabelas", fmt.Sprint(tables)},
			{"views", fmt.Sprint(views)},
			{"índices", fmt.Sprint(indexes)},
			{"funções", fmt.Sprint(funcs)},
			{"fks", fmt.Sprint(fks)},
		},
	}, nil
}

func (s *pgSession) Collections(ctx context.Context, exact bool) ([]Collection, error) {
	rows, err := s.pool.Query(ctx, tablesSQL, exact)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Collection
	for rows.Next() {
		var schema, name, size, orderCol string
		var count int64
		var byDate bool
		if err := rows.Scan(&schema, &name, &count, &size, &orderCol, &byDate); err != nil {
			return nil, err
		}
		hint := orderHint(orderCol, byDate)
		namespace := schema
		if namespace == "public" {
			namespace = ""
		}
		d := pgDescriptor{OrderCol: orderCol, ByDate: byDate}
		out = append(out, Collection{
			Namespace:  namespace,
			Name:       name,
			Count:      count,
			Size:       size,
			Hint:       hint,
			Preview:    pgRecentQuery(schema, name, d),
			Descriptor: d,
		})
	}
	return out, rows.Err()
}

func (s *pgSession) Recent(ctx context.Context, c Collection) (*ResultSet, error) {
	d, _ := c.Descriptor.(pgDescriptor)
	schema := c.Namespace
	if schema == "" {
		schema = "public"
	}
	return s.runQuery(ctx, pgRecentQuery(schema, c.Name, d))
}

func (s *pgSession) Query(ctx context.Context, raw string) (*ResultSet, error) {
	return s.runQuery(ctx, raw)
}

func (s *pgSession) runQuery(ctx context.Context, sql string) (*ResultSet, error) {
	start := time.Now()
	rows, err := s.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rs := &ResultSet{Query: sql, Language: "sql"}
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

func (s *pgSession) ConnectHint() ConnectHint {
	dsn := s.cont.DSN()
	return ConnectHint{
		Name:      s.cont.Name,
		DSN:       dsn,
		Shell:     fmt.Sprintf("psql %q", dsn),
		ExecShell: fmt.Sprintf("docker exec -it %s psql -U %s -d %s", s.cont.Name, s.cont.User, s.cont.DB),
		Remove:    fmt.Sprintf("docker rm -f %s", s.cont.Name),
		Port:      s.cont.Port,
	}
}

func (s *pgSession) Restore() *RestoreResult { return s.restore }

func (s *pgSession) Close() error {
	s.pool.Close()
	s.cont.Remove()
	return nil
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
