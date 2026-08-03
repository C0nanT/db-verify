//go:build docker

package main

// Fixture de conformidade da engine SQLite (ver conformance_test.go): como
// gerar o backup mínimo válido e o backup truncado que TestEngineConformance
// exige de toda engine registrada.
//
// Ao contrário das demais fixtures deste arquivo (mysql_conformance_test.go,
// mariadb_conformance_test.go, redis_conformance_test.go…), gerar um banco
// SQLite não precisa de container nenhum — é só abrir o arquivo com o
// próprio driver in-process (sqlite.go usa o mesmo). A fixture continua
// atrás da build tag "docker" só porque conformance_test.go (o corpo de
// teste genérico que a consome) está — não porque a engine em si precise de
// Docker; TestEngineConformance chama requireDocker(t) antes de qualquer
// engine, sqlite incluída, o que é uma limitação da suíte compartilhada, não
// do runtime desta engine (ver sqlite_test.go, que já cobre o fluxo inteiro
// sem Docker nenhum).

import (
	"database/sql"
	"os"
	"testing"
	"time"
)

func init() {
	registerConformanceFixture("sqlite", ConformanceFixture{
		BuildValid:     sqliteConformanceValidBackup,
		BuildTruncated: sqliteConformanceTruncatedBackup,
		ValidQuery:     "SELECT 1",
		InvalidQuery:   "ISTO NÃO É SQL ;;;",
	})
}

// sqliteConformanceSourceDump cria o backup mínimo exigido pela suíte de
// conformidade: duas coleções, "com_dados" (25 linhas, com coluna de data
// para exercitar o limite de 20 de Recent e a ordem decrescente) e "vazia"
// (zero linhas, sem coluna de data) — mesmo schema das demais fixtures
// relacionais.
func sqliteConformanceSourceDump(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/conformance.sqlite"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("abrindo banco de conformidade: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE com_dados (id INTEGER PRIMARY KEY, created_at DATETIME NOT NULL)`,
		`CREATE TABLE vazia (id INTEGER PRIMARY KEY, label TEXT NOT NULL)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("falha ao aplicar schema de conformidade: %v", err)
		}
	}

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 25; i++ {
		ts := base.Add(time.Duration(i) * time.Hour).Format("2006-01-02 15:04:05")
		if _, err := db.Exec(`INSERT INTO com_dados (created_at) VALUES (?)`, ts); err != nil {
			t.Fatalf("falha ao popular linha de conformidade: %v", err)
		}
	}
	return path
}

// sqliteConformanceValidBackup implementa ConformanceFixture.BuildValid
// para o SQLite.
func sqliteConformanceValidBackup(t *testing.T) ConformanceBackup {
	t.Helper()
	return ConformanceBackup{
		Path: sqliteConformanceSourceDump(t),
		WantCollections: map[string]int64{
			"com_dados": 25,
			"vazia":     0,
		},
		DateCollection: "com_dados",
		DateColumn:     "created_at",
	}
}

// sqliteConformanceTruncatedBackup implementa
// ConformanceFixture.BuildTruncated para o SQLite: pega um banco válido e
// corta o arquivo pela metade, preservando o cabeçalho de 100 bytes (magic
// reconhecível) mas destruindo as páginas de dados — PRAGMA integrity_check
// (ou a própria abertura do banco, se o corte cair antes de qualquer página
// legível) detecta o corte e reporta erro, em vez de aceitar silenciosamente
// um arquivo incompleto.
func sqliteConformanceTruncatedBackup(t *testing.T) string {
	t.Helper()
	valid := sqliteConformanceSourceDump(t)

	data, err := os.ReadFile(valid)
	if err != nil {
		t.Fatalf("falha ao ler banco válido: %v", err)
	}
	if len(data) < 2048 {
		t.Fatalf("banco de conformidade menor do que esperado para truncar (%d bytes)", len(data))
	}
	cut := len(data) / 2

	truncPath := t.TempDir() + "/truncated.sqlite"
	if err := os.WriteFile(truncPath, data[:cut], 0o644); err != nil {
		t.Fatalf("falha ao escrever banco truncado: %v", err)
	}
	return truncPath
}
