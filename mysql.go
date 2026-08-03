package main

// Implementação da engine MySQL atrás da interface Engine/Session. Toda a
// variação específica de MySQL (mysqldump texto, information_schema,
// "database.tabela", mysql client) fica contida neste arquivo — main.go e a
// TUI só falam com Engine/Session. A heurística de coluna de ordenação
// reusa a lista compartilhada de relational.go, só traduzida para os tipos
// de data do MySQL; a decisão em si (qual coluna vence) é feita em Go em vez
// de SQL porque o MySQL não tem um jeito portável de "melhor por grupo" sem
// depender de CTE/window functions (que exigem 8.0+, e o backup pode ser de
// um 5.7).
//
// mariadbEngine (mariadb.go) reusa inteiramente mysqlContainer e
// mysqlSession — protocolo de fio, information_schema e heurística de coluna
// são idênticos entre as duas engines — variando só Image e Client
// (mysqlContainer.Client) e a versão default de imagem. Não há duplicação da
// heurística de coluna nem das consultas de introspecção: mariadb.go não
// declara nenhuma.

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	_ "github.com/go-sql-driver/mysql"
)

func init() { Register(mysqlEngine{}) }

// mysqlEngine implementa Engine para MySQL.
type mysqlEngine struct{}

func (mysqlEngine) Name() string { return "mysql" }

var (
	// reMySQLDumpHeader reconhece o cabeçalho de texto que o mysqldump
	// sempre escreve na primeira linha. Distinto do cabeçalho do MariaDB
	// ("-- MariaDB dump", ver mariadb.go), que é como as duas engines nunca
	// disputam o mesmo dump com cabeçalho reconhecível.
	reMySQLDumpHeader = regexp.MustCompile(`-- MySQL dump\b`)

	// reMySQLFamilyServerVer, reMySQLFamilyDistribVer e
	// reMySQLFamilyDatabaseLine reconhecem as mesmas linhas de metadado no
	// corpo do dump (versão do servidor, versão do cliente que gerou o dump,
	// banco de origem) — o formato é idêntico entre mysqldump e
	// mariadb-dump, só o texto do cabeçalho de identificação muda.
	// Compartilhadas entre mysql.go e mariadb.go para a extração de versão
	// não divergir entre as duas engines.
	reMySQLFamilyServerVer    = regexp.MustCompile(`(?m)^-- Server version\s+(\d+\.\d+)`)
	reMySQLFamilyDistribVer   = regexp.MustCompile(`Distrib (\d+\.\d+)`)
	reMySQLFamilyDatabaseLine = regexp.MustCompile(`(?m)^-- Host: \S+\s+Database: (\S+)`)
)

// defaultMySQLVersion é o fallback documentado (ticket 05) quando nem o
// cabeçalho "-- Server version" nem "Distrib" informam a versão de origem:
// a imagem estável mais recente da série 8, compatível com a maioria dos
// dumps em uso.
const defaultMySQLVersion = "8.0"

// resolveMySQLFamilyVersion decide a tag da imagem: --version-tag explícito
// primeiro, depois a versão extraída do próprio dump (Detect), e só então o
// fallback de cada engine (defaultMySQLVersion/defaultMariaDBVersion).
// Compartilhada entre mysqlResolveVersion e mariadbResolveVersion para a
// ordem de precedência não divergir entre as duas; extraída à parte de
// Provision para ser testável sem Docker.
func resolveMySQLFamilyVersion(versionTag, backupVersion, fallback string) string {
	if versionTag != "" {
		return versionTag
	}
	if backupVersion != "" {
		return backupVersion
	}
	return fallback
}

func mysqlResolveVersion(versionTag, backupVersion string) string {
	return resolveMySQLFamilyVersion(versionTag, backupVersion, defaultMySQLVersion)
}

// Detect reconhece um mysqldump em texto plano pelo cabeçalho "-- MySQL
// dump", e extrai versão (preferindo "-- Server version", caindo para
// "Distrib" quando ausente) e banco de origem da linha "-- Host: ...
// Database: ...". Sem cabeçalho reconhecível, a extensão .sql ainda conta
// como sinal de confiança média (SPEC.md, tabela de assinaturas).
func (mysqlEngine) Detect(head []byte, path string) (Match, bool) {
	if reMySQLDumpHeader.Match(head) {
		m := Match{Format: "sql", Confidence: ConfidenceMagic}
		if mm := reMySQLFamilyServerVer.FindSubmatch(head); mm != nil {
			m.Version = string(mm[1])
		} else if mm := reMySQLFamilyDistribVer.FindSubmatch(head); mm != nil {
			m.Version = string(mm[1])
		}
		if mm := reMySQLFamilyDatabaseLine.FindSubmatch(head); mm != nil {
			m.OriginDB = string(mm[1])
		}
		return m, true
	}
	if strings.HasSuffix(strings.ToLower(path), ".sql") {
		return Match{Format: "sql", Confidence: ConfidenceExtension}, true
	}
	return Match{}, false
}

// Expects descreve o que o MySQL reconhece, para mensagens de erro e
// --list-engines.
func (mysqlEngine) Expects() string {
	return "dumps do mysqldump: cabeçalho \"-- MySQL dump\", ou extensão .sql"
}

// Provision sobe o container, espera ficar pronto, copia o dump, restaura e
// conecta — mesmo formato grosso de pgEngine.Provision, para o número de
// seams continuar sendo um.
func (mysqlEngine) Provision(ctx context.Context, b *Backup, opts ProvisionOpts) (Session, error) {
	if err := dockerAvailable(); err != nil {
		return nil, err
	}

	version := mysqlResolveVersion(opts.VersionTag, b.Version)
	port := opts.Port
	if port == 0 {
		port = freePortFrom(mysqlDefaultPort)
	}
	db := opts.DBName
	if db == "" {
		db = "verify"
	}

	cont := &mysqlContainer{
		Name:   fmt.Sprintf("db-verify-%d", os.Getpid()),
		Image:  "mysql:" + version,
		Client: "mysql",
		Port:   port, DB: db, User: "root", Pass: "root",
	}

	opts.report("subindo container %s (imagem %s)…", cont.Name, cont.Image)
	if err := cont.Start(ctx); err != nil {
		return nil, err
	}
	opts.report("aguardando o MySQL ficar pronto…")
	if err := cont.WaitReady(ctx, 120*time.Second); err != nil {
		cont.Remove()
		return nil, err
	}
	opts.report("copiando dump para o container…")
	if err := cont.CopyDump(ctx, b); err != nil {
		cont.Remove()
		return nil, err
	}
	opts.report("restaurando (pode demorar)…")
	res, err := cont.Restore(ctx)
	if err != nil {
		cont.Remove()
		return nil, err
	}

	conn, err := mysqlConnect(cont.DSN())
	if err != nil {
		cont.Remove()
		return nil, fmt.Errorf("conexão falhou: %w", err)
	}

	return &mysqlSession{db: conn, cont: cont, restore: res}, nil
}

// ---------------------------------------------------------- container ---

const mysqlDefaultPort = 3306

// mysqlContainer representa o container temporário — MySQL ou MariaDB — usado
// para validar o backup. Compartilhado entre as duas engines (mysql.go,
// mariadb.go): o protocolo de fio, o information_schema e o cliente de linha
// de comando (invocado via Client) são compatíveis o bastante para uma única
// implementação servir as duas, com Image e Client sendo o que cada engine
// decide de diferente (ver SPEC.md, tabela "Por engine").
type mysqlContainer struct {
	Name  string
	Image string
	// Client é o binário do cliente de linha de comando usado para o
	// health-check de prontidão, o restore e o shell de --keep: "mysql"
	// para a engine MySQL, "mariadb" para a engine MariaDB.
	Client string
	Port   int
	DB     string
	User   string
	Pass   string
}

// DSN usa parseTime=true para que colunas DATE/DATETIME/TIMESTAMP cheguem
// como time.Time (formatValue as formata igual às do Postgres), em vez de
// texto cru.
func (c *mysqlContainer) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(127.0.0.1:%d)/%s?parseTime=true&multiStatements=true",
		c.User, c.Pass, c.Port, c.DB)
}

// Start sobe o container com durabilidade relaxada — dado descartável, o
// restore precisa ser rápido, não seguro contra crash.
func (c *mysqlContainer) Start(ctx context.Context) error {
	args := []string{
		"run", "-d", "--name", c.Name,
		"-e", "MYSQL_ROOT_PASSWORD=" + c.Pass,
		"-e", "MYSQL_DATABASE=" + c.DB,
		"-p", fmt.Sprintf("127.0.0.1:%d:3306", c.Port),
		c.Image,
		"--innodb-flush-log-at-trx-commit=0",
		"--innodb-doublewrite=0",
		"--skip-log-bin",
	}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("falha ao subir container: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// WaitReady espera até uma consulta autenticada de verdade funcionar duas
// vezes seguidas — não basta o "mysqladmin ping" responder: a imagem
// oficial sobe um "servidor temporário" para rodar a inicialização (que já
// responde ping, sem a senha de root definitiva) e só depois reinicia para
// o servidor real. Um ping bem-sucedido contra o temporário dá falso
// positivo (o restore que vem em seguida falharia com "Access denied"), e
// testar bem na fronteira do reinício pode achar o socket momentaneamente
// fechado logo depois de uma consulta ter funcionado — daí a segunda
// checagem, com um respiro entre elas.
func (c *mysqlContainer) WaitReady(ctx context.Context, timeout time.Duration) error {
	check := func() bool {
		return exec.CommandContext(ctx, "docker", "exec", c.Name,
			c.Client, "-u"+c.User, "-p"+c.Pass, "-e", "SELECT 1").Run() == nil
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
			if check() {
				return nil
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	logs, _ := exec.Command("docker", "logs", "--tail", "20", c.Name).CombinedOutput()
	return fmt.Errorf("timeout esperando o %s:\n%s", c.Client, string(logs))
}

// CopyDump joga o arquivo dentro do container, descomprimindo se preciso —
// sempre por stream, nunca duplicando o dump inteiro em disco no host.
func (c *mysqlContainer) CopyDump(ctx context.Context, b *Backup) error {
	if b.Compression == "none" {
		out, err := exec.CommandContext(ctx, "docker", "cp", b.Path, c.Name+":/tmp/backup.sql").CombinedOutput()
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

	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", c.Name, "sh", "-c", "cat > /tmp/backup.sql")
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

// reMySQLFamilyRestoreErr reconhece as linhas de erro do cliente mysql/
// mariadb: "ERROR 1146 (42S02) at line 25: ..." — o formato que os dois
// clientes escrevem tanto em modo interativo quanto ao consumir um arquivo
// com --force (o cliente mariadb é compatível byte a byte aqui).
var reMySQLFamilyRestoreErr = regexp.MustCompile(`(?m)^ERROR\b`)

// Restore roda o cliente (c.Client: "mysql" ou "mariadb") consumindo o dump
// copiado, com --force para não abortar no primeiro erro (equivalente ao
// ON_ERROR_STOP=0 do Postgres para dumps plain) — assim um backup truncado
// produz todos os erros, não só o primeiro.
func (c *mysqlContainer) Restore(ctx context.Context) (*RestoreResult, error) {
	start := time.Now()
	shell := fmt.Sprintf("%s --force -u%s -p%s %s < /tmp/backup.sql", c.Client, c.User, c.Pass, c.DB)
	out, err := exec.CommandContext(ctx, "docker", "exec", c.Name, "sh", "-c", shell).CombinedOutput()

	res := &RestoreResult{Duration: time.Since(start)}
	if ee, ok := err.(*exec.ExitError); ok {
		res.ExitCode = ee.ExitCode()
	} else if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if reMySQLFamilyRestoreErr.MatchString(line) {
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

func (c *mysqlContainer) Remove() {
	exec.Command("docker", "rm", "-f", c.Name).Run()
}

// ------------------------------------------------------------- session ---

func mysqlConnect(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	return db, nil
}

// mysqlDateTypes são os tipos de coluna do MySQL equivalentes ao "timestamp
// with time zone"/"timestamp without time zone"/"date" do Postgres — a
// última camada da heurística, quando nenhum nome conhecido casa.
var mysqlDateTypes = map[string]bool{"timestamp": true, "datetime": true, "date": true}

// columnPref devolve a preferência da coluna na heurística compartilhada
// (orderColumnTiers, relational.go): 1..3 para os nomes conhecidos, em
// ordem, 4 para qualquer outra coluna de data/hora, 9 para o resto (não
// candidata a ordenação por data).
func columnPref(name, dataType string) int {
	for i, tier := range orderColumnTiers {
		for _, n := range tier {
			if name == n {
				return i + 1
			}
		}
	}
	if mysqlDateTypes[dataType] {
		return 4
	}
	return 9
}

// chooseOrderColumn aplica a heurística compartilhada às colunas de uma
// tabela: a de menor preferência (columnPref) vence; sem nenhuma candidata a
// data, cai para a PK simples (pk, "" se não houver); sem nenhuma das duas,
// a tabela não tem coluna de ordenação.
func chooseOrderColumn(cols []mysqlColumn, pk string) (orderCol string, byDate bool) {
	best := 9
	for _, c := range cols {
		if p := columnPref(c.Name, c.DataType); p < best {
			best, orderCol = p, c.Name
		}
	}
	if orderCol != "" {
		return orderCol, best <= 4
	}
	if pk != "" {
		return pk, false
	}
	return "", false
}

// mysqlDescriptor é o Descriptor opaco que Collections anexa a cada
// Collection e que Recent usa para montar o SELECT — só a engine MySQL sabe
// o que esses campos significam.
type mysqlDescriptor struct {
	OrderCol string
	ByDate   bool
}

func mysqlIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

// mysqlRecentQuery monta o SELECT dos 20 registros mais recentes da tabela.
func mysqlRecentQuery(db, table string, d mysqlDescriptor) string {
	if d.OrderCol == "" {
		return fmt.Sprintf("SELECT * FROM %s.%s LIMIT 20;", mysqlIdent(db), mysqlIdent(table))
	}
	return fmt.Sprintf("SELECT * FROM %s.%s ORDER BY %s DESC LIMIT 20;",
		mysqlIdent(db), mysqlIdent(table), mysqlIdent(d.OrderCol))
}

type mysqlSession struct {
	db      *sql.DB
	cont    *mysqlContainer
	restore *RestoreResult
}

const mysqlHealthSQL = `SELECT DATABASE(),
  COALESCE((SELECT SUM(data_length + index_length) FROM information_schema.tables WHERE table_schema = DATABASE()), 0),
  (SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'),
  (SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'VIEW'),
  (SELECT COUNT(*) FROM (SELECT DISTINCT table_name, index_name FROM information_schema.statistics WHERE table_schema = DATABASE()) x),
  (SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema = DATABASE() AND constraint_type = 'FOREIGN KEY'),
  (SELECT COUNT(*) FROM information_schema.routines WHERE routine_schema = DATABASE()),
  (SELECT COUNT(*) FROM information_schema.triggers WHERE trigger_schema = DATABASE())`

func (s *mysqlSession) Health(ctx context.Context) (*Health, error) {
	var name string
	var sizeBytes int64
	var tables, views, indexes, fks, procs, triggers int
	err := s.db.QueryRowContext(ctx, mysqlHealthSQL).Scan(
		&name, &sizeBytes, &tables, &views, &indexes, &fks, &procs, &triggers)
	if err != nil {
		return nil, err
	}
	return &Health{
		Name: name,
		Size: humanSize(sizeBytes),
		Fields: []HealthField{
			{"tabelas", fmt.Sprint(tables)},
			{"views", fmt.Sprint(views)},
			{"índices", fmt.Sprint(indexes)},
			{"fks", fmt.Sprint(fks)},
			{"procedures", fmt.Sprint(procs)},
			{"gatilhos", fmt.Sprint(triggers)},
		},
	}, nil
}

type mysqlColumn struct {
	Name     string
	DataType string
}

const mysqlTablesSQL = `SELECT table_name, table_rows, data_length + index_length
FROM information_schema.tables
WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'
ORDER BY table_name`

const mysqlColumnsSQL = `SELECT table_name, column_name, data_type
FROM information_schema.columns
WHERE table_schema = DATABASE()
ORDER BY table_name, ordinal_position`

// mysqlSinglePKSQL lista, por tabela, a coluna de PK quando ela é composta
// por uma única coluna — mesma restrição do pk do Postgres ("simples").
const mysqlSinglePKSQL = `
SELECT k.table_name, MIN(k.column_name)
FROM information_schema.key_column_usage k
JOIN information_schema.table_constraints t
  ON t.constraint_schema = k.constraint_schema AND t.constraint_name = k.constraint_name AND t.table_name = k.table_name
WHERE k.table_schema = DATABASE() AND t.constraint_type = 'PRIMARY KEY'
GROUP BY k.table_name
HAVING COUNT(*) = 1`

func (s *mysqlSession) Collections(ctx context.Context, exact bool) ([]Collection, error) {
	columnsByTable := map[string][]mysqlColumn{}
	crows, err := s.db.QueryContext(ctx, mysqlColumnsSQL)
	if err != nil {
		return nil, err
	}
	for crows.Next() {
		var table, col, dataType string
		if err := crows.Scan(&table, &col, &dataType); err != nil {
			crows.Close()
			return nil, err
		}
		columnsByTable[table] = append(columnsByTable[table], mysqlColumn{Name: col, DataType: dataType})
	}
	if err := crows.Err(); err != nil {
		crows.Close()
		return nil, err
	}
	crows.Close()

	pkByTable := map[string]string{}
	prows, err := s.db.QueryContext(ctx, mysqlSinglePKSQL)
	if err != nil {
		return nil, err
	}
	for prows.Next() {
		var table, col string
		if err := prows.Scan(&table, &col); err != nil {
			prows.Close()
			return nil, err
		}
		pkByTable[table] = col
	}
	if err := prows.Err(); err != nil {
		prows.Close()
		return nil, err
	}
	prows.Close()

	trows, err := s.db.QueryContext(ctx, mysqlTablesSQL)
	if err != nil {
		return nil, err
	}
	defer trows.Close()

	var out []Collection
	for trows.Next() {
		var table string
		var estRows sql.NullInt64
		var sizeBytes sql.NullInt64
		if err := trows.Scan(&table, &estRows, &sizeBytes); err != nil {
			return nil, err
		}

		count := estRows.Int64
		if exact {
			if err := s.db.QueryRowContext(ctx,
				fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", mysqlIdent(s.cont.DB), mysqlIdent(table)),
			).Scan(&count); err != nil {
				return nil, err
			}
		}

		orderCol, byDate := chooseOrderColumn(columnsByTable[table], pkByTable[table])
		d := mysqlDescriptor{OrderCol: orderCol, ByDate: byDate}
		out = append(out, Collection{
			Namespace:  s.cont.DB,
			Name:       table,
			Count:      count,
			Size:       humanSize(sizeBytes.Int64),
			Hint:       orderHint(orderCol, byDate),
			Preview:    mysqlRecentQuery(s.cont.DB, table, d),
			Descriptor: d,
		})
	}
	return out, trows.Err()
}

func (s *mysqlSession) Recent(ctx context.Context, c Collection) (*ResultSet, error) {
	d, _ := c.Descriptor.(mysqlDescriptor)
	return s.runQuery(ctx, mysqlRecentQuery(s.cont.DB, c.Name, d))
}

func (s *mysqlSession) Query(ctx context.Context, raw string) (*ResultSet, error) {
	return s.runQuery(ctx, raw)
}

func (s *mysqlSession) runQuery(ctx context.Context, query string) (*ResultSet, error) {
	start := time.Now()
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	rs := &ResultSet{Query: query, Language: "sql", Columns: cols}

	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = mysqlFormatValue(v)
		}
		rs.Rows = append(rs.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rs.Elapsed = time.Since(start)
	return rs, nil
}

func (s *mysqlSession) ConnectHint() ConnectHint {
	shell := fmt.Sprintf("%s -h127.0.0.1 -P%d -u%s -p%s %s", s.cont.Client, s.cont.Port, s.cont.User, s.cont.Pass, s.cont.DB)
	return ConnectHint{
		Name:      s.cont.Name,
		DSN:       s.cont.DSN(),
		Shell:     shell,
		ExecShell: fmt.Sprintf("docker exec -it %s %s -u%s -p%s %s", s.cont.Name, s.cont.Client, s.cont.User, s.cont.Pass, s.cont.DB),
		Remove:    fmt.Sprintf("docker rm -f %s", s.cont.Name),
		Port:      s.cont.Port,
	}
}

func (s *mysqlSession) Restore() *RestoreResult { return s.restore }

func (s *mysqlSession) Close() error {
	s.db.Close()
	s.cont.Remove()
	return nil
}

// mysqlFormatValue formata um valor devolvido pelo driver do MySQL. Ao
// contrário do pgx (Postgres), o driver database/sql do MySQL devolve texto
// e decimais como []byte — tratado aqui como string UTF-8 (o caso comum);
// só quando o conteúdo não é UTF-8 válido (BLOB de verdade) é que cai para
// hexadecimal, como o Postgres faz para bytea.
func mysqlFormatValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "∅"
	case time.Time:
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 {
			return x.Format("2006-01-02")
		}
		return x.Format("2006-01-02 15:04:05")
	case []byte:
		if utf8.Valid(x) {
			return strings.Join(strings.Fields(string(x)), " ")
		}
		return fmt.Sprintf("\\x%x", x)
	case string:
		return strings.Join(strings.Fields(x), " ")
	default:
		return strings.Join(strings.Fields(fmt.Sprintf("%v", x)), " ")
	}
}
