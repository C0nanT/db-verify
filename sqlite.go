package main

// Implementação da engine SQLite atrás da interface Engine/Session (ticket
// 08). É a única engine sem container: SQLite é um banco embarcado, então
// "provisionar" não significa subir servidor nenhum, significa copiar o
// arquivo para um temporário (nunca escrever no original do usuário) e
// abri-lo direto via driver in-process. O "restore" equivalente ao
// pg_restore/mysql das demais engines é essa cópia mais um
// `PRAGMA integrity_check`, cujo resultado alimenta RestoreResult.Errors do
// mesmo jeito que os erros de um cliente de linha de comando alimentam as
// outras engines — main.go e a suíte de conformidade não precisam saber que
// não houve container nenhum.
//
// A heurística de coluna de ordenação reusa mysqlColumn/chooseOrderColumn de
// mysql.go: mesma lista compartilhada (relational.go), mesma decisão em Go
// em vez de SQL — SQLite também não tem um jeito portável de "melhor coluna
// por tabela" via CTE que valha a pena reimplementar.
//
// Driver: modernc.org/sqlite (puro Go, sem cgo) — importante para o caso que
// esta engine existe para cobrir (SPEC.md, "ambiente sem Docker"): o
// binário do db-verify continua se bastando sozinho, sem depender de um
// toolchain C no host para compilar ou rodar.

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

func init() { Register(sqliteEngine{}) }

// sqliteEngine implementa Engine para SQLite.
type sqliteEngine struct{}

func (sqliteEngine) Name() string { return "sqlite" }

// sqliteMagic é o cabeçalho fixo de 16 bytes que todo arquivo SQLite 3
// carrega no início (formato documentado publicamente, nunca muda de
// versão para versão).
var sqliteMagic = []byte("SQLite format 3\x00")

// Detect reconhece um banco SQLite pelo magic de 16 bytes, com fallback de
// confiança média para as extensões .sqlite/.db — as duas mais usadas na
// prática, nenhuma exclusiva de SQLite (SPEC.md, tabela de assinaturas).
func (sqliteEngine) Detect(head []byte, path string) (Match, bool) {
	if bytes.HasPrefix(head, sqliteMagic) {
		return Match{Format: "sqlite3", Confidence: ConfidenceMagic}, true
	}
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".sqlite") || strings.HasSuffix(lower, ".db") {
		return Match{Format: "sqlite3", Confidence: ConfidenceExtension}, true
	}
	return Match{}, false
}

// Expects descreve o que o SQLite reconhece, para mensagens de erro e
// --list-engines.
func (sqliteEngine) Expects() string {
	return `arquivos SQLite: magic "SQLite format 3\0", ou extensão .sqlite/.db`
}

// Provision não sobe container nenhum (SPEC.md, "SQLite não usa Docker"):
// copia o backup (descomprimindo se preciso) para um arquivo temporário —
// a verificação nunca toca no arquivo original — e roda um
// PRAGMA integrity_check nessa cópia, cujo resultado vira o RestoreResult
// que as demais engines produzem rodando um cliente de linha de comando.
// Instantâneo por natureza: sem download de imagem, sem esperar servidor
// ficar pronto.
func (sqliteEngine) Provision(ctx context.Context, b *Backup, opts ProvisionOpts) (Session, error) {
	opts.report("copiando arquivo para um temporário (o original nunca é tocado)…")
	tmpPath, err := sqliteCopyToTemp(b)
	if err != nil {
		return nil, fmt.Errorf("copiando para temporário: %w", err)
	}

	db, err := sql.Open("sqlite", tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("abrindo cópia: %w", err)
	}

	opts.report("checando integridade…")
	res, err := sqliteIntegrityCheck(ctx, db)
	if err != nil {
		db.Close()
		os.Remove(tmpPath)
		return nil, err
	}

	return &sqliteSession{
		db:       db,
		tmpPath:  tmpPath,
		origName: filepath.Base(b.Path),
		restore:  res,
	}, nil
}

// sqliteCopyToTemp descomprime o backup se preciso (openMaybeCompressed já
// cuida de gzip/zstd/bzip2/nenhum de forma uniforme) e o copia por stream
// para um arquivo novo, temporário — nunca abre nem grava no arquivo
// original apontado por b.Path.
func sqliteCopyToTemp(b *Backup) (string, error) {
	r, _, err := openMaybeCompressed(b.Path)
	if err != nil {
		return "", err
	}
	defer r.Close()

	f, err := os.CreateTemp("", "db-verify-sqlite-*.db")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// sqliteIntegrityCheck roda PRAGMA integrity_check na cópia e traduz o
// resultado para RestoreResult: uma única linha "ok" vira zero erros;
// qualquer outra linha (uma por problema encontrado) vira um item de
// Errors; um arquivo que não é sequer um banco SQLite válido (magic errado,
// truncado bem no cabeçalho) faz a própria consulta falhar, e essa falha
// também vira um erro reportado — nunca um pânico ou um erro genérico de
// Provision indistinguível de "faltou Docker". Como as demais engines,
// grava o log completo em arquivo quando há erros.
func sqliteIntegrityCheck(ctx context.Context, db *sql.DB) (*RestoreResult, error) {
	start := time.Now()
	res := &RestoreResult{}

	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		res.Errors = []string{err.Error()}
	} else {
		for rows.Next() {
			var line string
			if serr := rows.Scan(&line); serr != nil {
				rows.Close()
				return nil, serr
			}
			if line != "ok" {
				res.Errors = append(res.Errors, line)
			}
		}
		rerr := rows.Err()
		rows.Close()
		if rerr != nil {
			res.Errors = append(res.Errors, rerr.Error())
		}
	}

	res.Duration = time.Since(start)
	if len(res.Errors) > 0 {
		if f, e := os.CreateTemp("", "db-verify-*.log"); e == nil {
			f.WriteString(strings.Join(res.Errors, "\n"))
			f.Close()
			res.LogPath = f.Name()
		}
	}
	return res, nil
}

// ------------------------------------------------------------- session ---

// sqliteDescriptor é o Descriptor opaco que Collections anexa a cada
// Collection e que Recent usa para montar o SELECT.
type sqliteDescriptor struct {
	OrderCol string
	ByDate   bool
}

// sqliteIdent quota um identificador (tabela/coluna) ao estilo SQL padrão —
// aspas duplas, com qualquer aspa dupla embutida dobrada.
func sqliteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// sqliteStringLiteral quota um literal de string — usado só para o
// argumento de PRAGMAs (table_info/foreign_key_list), que aceitam o nome da
// tabela como string entre aspas simples.
func sqliteStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// sqliteRecentQuery monta o SELECT dos 20 registros mais recentes da
// tabela — sem schema/namespace: SQLite não tem um equivalente a
// "database.tabela" para um único arquivo.
func sqliteRecentQuery(table string, d sqliteDescriptor) string {
	if d.OrderCol == "" {
		return fmt.Sprintf("SELECT * FROM %s LIMIT 20;", sqliteIdent(table))
	}
	return fmt.Sprintf("SELECT * FROM %s ORDER BY %s DESC LIMIT 20;", sqliteIdent(table), sqliteIdent(d.OrderCol))
}

type sqliteSession struct {
	db       *sql.DB
	tmpPath  string
	origName string
	restore  *RestoreResult
}

// sqliteTableNames lista as tabelas de verdade do arquivo — sqlite_master
// também carrega as tabelas internas de contabilidade do próprio SQLite
// (sqlite_sequence, sqlite_stat1…), prefixadas com "sqlite_", que não são
// coleções do operador e por isso ficam de fora.
func (s *sqliteSession) sqliteTableNames(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// sqliteTableColumns devolve as colunas da tabela (nome + tipo declarado,
// já em minúsculo para casar com a heurística compartilhada de
// chooseOrderColumn) e o nome da coluna de PK quando ela é composta por uma
// única coluna — mesma restrição "simples" do Postgres/MySQL.
func (s *sqliteSession) sqliteTableColumns(ctx context.Context, table string) (cols []mysqlColumn, pk string, err error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", sqliteStringLiteral(table)))
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var pkCols []string
	for rows.Next() {
		var cid, notNull, pkPos int
		var name, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pkPos); err != nil {
			return nil, "", err
		}
		cols = append(cols, mysqlColumn{Name: name, DataType: strings.ToLower(colType)})
		if pkPos > 0 {
			pkCols = append(pkCols, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(pkCols) == 1 {
		pk = pkCols[0]
	}
	return cols, pk, nil
}

// sqliteTableSize soma o tamanho em disco (em páginas) da tabela via a
// tabela virtual dbstat. dbstat é um recurso opcional de build do SQLite;
// quando indisponível, o tamanho vira 0 em vez de um erro fatal — é
// metadado auxiliar, não algo que deva impedir a verificação de continuar.
func (s *sqliteSession) sqliteTableSize(ctx context.Context, table string) int64 {
	var size sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT SUM(pgsize) FROM dbstat WHERE name = ?", table).Scan(&size)
	if err != nil {
		return 0
	}
	return size.Int64
}

// sqliteForeignKeyCount soma o total de chaves estrangeiras declaradas em
// todas as tabelas, para o campo "fks" de Health — SQLite não tem uma visão
// agregada pronta como information_schema.table_constraints, só o PRAGMA
// por tabela.
func (s *sqliteSession) sqliteForeignKeyCount(ctx context.Context, tables []string) (int, error) {
	total := 0
	for _, table := range tables {
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf("PRAGMA foreign_key_list(%s)", sqliteStringLiteral(table)))
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			total++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

// Health mostra só os contadores que fazem sentido para um arquivo SQLite —
// sem "procedures" nem nada que o formato simplesmente não tem (ticket 08).
func (s *sqliteSession) Health(ctx context.Context) (*Health, error) {
	var tables, views, indexes, triggers int
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\'),
		(SELECT COUNT(*) FROM sqlite_master WHERE type = 'view'),
		(SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name NOT LIKE 'sqlite\_%' ESCAPE '\'),
		(SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger')`).
		Scan(&tables, &views, &indexes, &triggers)
	if err != nil {
		return nil, err
	}

	tableNames, err := s.sqliteTableNames(ctx)
	if err != nil {
		return nil, err
	}
	fks, err := s.sqliteForeignKeyCount(ctx, tableNames)
	if err != nil {
		return nil, err
	}

	st, err := os.Stat(s.tmpPath)
	if err != nil {
		return nil, err
	}

	return &Health{
		Name: s.origName,
		Size: humanSize(st.Size()),
		Fields: []HealthField{
			{"tabelas", fmt.Sprint(tables)},
			{"views", fmt.Sprint(views)},
			{"índices", fmt.Sprint(indexes)},
			{"gatilhos", fmt.Sprint(triggers)},
			{"fks", fmt.Sprint(fks)},
		},
	}, nil
}

// Collections lista as tabelas do arquivo. exact é ignorado deliberadamente:
// SQLite não tem um contador estimado tipo n_live_tup do Postgres, e
// COUNT(*) num arquivo local é barato o bastante para ser sempre exato.
func (s *sqliteSession) Collections(ctx context.Context, exact bool) ([]Collection, error) {
	names, err := s.sqliteTableNames(ctx)
	if err != nil {
		return nil, err
	}

	var out []Collection
	for _, name := range names {
		cols, pk, err := s.sqliteTableColumns(ctx, name)
		if err != nil {
			return nil, err
		}
		orderCol, byDate := chooseOrderColumn(cols, pk)

		var count int64
		if err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", sqliteIdent(name))).Scan(&count); err != nil {
			return nil, err
		}

		d := sqliteDescriptor{OrderCol: orderCol, ByDate: byDate}
		out = append(out, Collection{
			Name:       name,
			Count:      count,
			Size:       humanSize(s.sqliteTableSize(ctx, name)),
			Hint:       orderHint(orderCol, byDate),
			Preview:    sqliteRecentQuery(name, d),
			Descriptor: d,
		})
	}
	return out, nil
}

func (s *sqliteSession) Recent(ctx context.Context, c Collection) (*ResultSet, error) {
	d, _ := c.Descriptor.(sqliteDescriptor)
	return s.runQuery(ctx, sqliteRecentQuery(c.Name, d))
}

func (s *sqliteSession) Query(ctx context.Context, raw string) (*ResultSet, error) {
	return s.runQuery(ctx, raw)
}

func (s *sqliteSession) runQuery(ctx context.Context, query string) (*ResultSet, error) {
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
			row[i] = sqliteFormatValue(v)
		}
		rs.Rows = append(rs.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rs.Elapsed = time.Since(start)
	return rs, nil
}

// ConnectHint não nomeia container nenhum (Name fica vazio, de propósito: é
// assim que o resto do programa e a suíte de conformidade sabem que esta
// engine não tem container para inspecionar/remover). DSN e Remove apontam
// para o arquivo temporário — é isso que --keep preserva e imprime.
func (s *sqliteSession) ConnectHint() ConnectHint {
	return ConnectHint{
		DSN:    s.tmpPath,
		Shell:  fmt.Sprintf("sqlite3 %s", s.tmpPath),
		Remove: fmt.Sprintf("rm %s", s.tmpPath),
	}
}

func (s *sqliteSession) Restore() *RestoreResult { return s.restore }

// Close fecha o banco e remove a cópia temporária — o arquivo original do
// usuário nunca foi tocado, então não há nada mais para desfazer.
func (s *sqliteSession) Close() error {
	err := s.db.Close()
	if rmErr := os.Remove(s.tmpPath); err == nil {
		err = rmErr
	}
	return err
}

// sqliteFormatValue formata um valor devolvido pelo driver modernc.org/sqlite.
// Colunas com afinidade de data (tipo declarado DATE/DATETIME/TIMESTAMP) já
// chegam como time.Time, formatadas igual às demais engines; BLOBs não-UTF8
// caem em hexadecimal, como bytea do Postgres.
func sqliteFormatValue(v any) string {
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
