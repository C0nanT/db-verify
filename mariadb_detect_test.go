package main

// Detecção do MariaDB contra as fixtures reais em testdata/headers: precisa do
// conjunto de engines montado na raiz, então não mora em engines/mysql.

import (
	"testing"

	"db-verify/internal/detect"
)

// TestMariaDBDetect_Header caracteriza a detecção pelo cabeçalho de texto do
// mariadb-dump: magic bytes, versão (preferindo "Server version", caindo
// para "Distrib" quando ausente) e banco de origem.
func TestMariaDBDetect_Header(t *testing.T) {
	cases := []struct {
		name        string
		path        string
		wantVersion string
		wantOrigin  string
	}{
		{"com Server version", "testdata/headers/mariadb.sql", "10.6", "fixturedb"},
		{"só Distrib", "testdata/headers/mariadb-distrib-only.sql", "10.5", "legacydb"},
		{"sem nenhuma versão", "testdata/headers/mariadb-no-version.sql", "", "outrodb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := detect.InspectDump(tc.path)
			if err != nil {
				t.Fatalf("InspectDump(%q): %v", tc.path, err)
			}
			if info.Engine != "mariadb" {
				t.Fatalf("Engine = %q, want mariadb", info.Engine)
			}
			if info.Format != "sql" {
				t.Errorf("Format = %q, want sql", info.Format)
			}
			if info.Guessed {
				t.Errorf("esperava Guessed=false para cabeçalho reconhecido por magic bytes")
			}
			if info.Version != tc.wantVersion {
				t.Errorf("Version = %q, want %q", info.Version, tc.wantVersion)
			}
			if info.OriginDB != tc.wantOrigin {
				t.Errorf("OriginDB = %q, want %q", info.OriginDB, tc.wantOrigin)
			}
		})
	}
}

// TestMariaDBDetect_Gzip caracteriza a detecção através de gzip: o
// cabeçalho é descomprimido antes de a engine olhar para ele, então o
// mariadb-dump gzipado é reconhecido igual ao plano.
func TestMariaDBDetect_Gzip(t *testing.T) {
	info, err := detect.InspectDump("testdata/headers/mariadb.sql.gz")
	if err != nil {
		t.Fatalf("InspectDump: %v", err)
	}
	if info.Compression != "gzip" {
		t.Errorf("Compression = %q, want gzip", info.Compression)
	}
	if info.Engine != "mariadb" {
		t.Errorf("Engine = %q, want mariadb", info.Engine)
	}
	if info.Version != "10.6" {
		t.Errorf("Version = %q, want 10.6", info.Version)
	}
}

// TestMySQLDetect_DumpDeVerdadeContinuaMySQL caracteriza a coexistência das
// duas engines no registro (ticket 06): um dump MySQL de verdade continua
// sendo detectado como mysql, não mariadb, mesmo com a engine mariadb
// registrada.
func TestMySQLDetect_DumpDeVerdadeContinuaMySQL(t *testing.T) {
	info, err := detect.InspectDump("testdata/headers/mysql.sql")
	if err != nil {
		t.Fatalf("InspectDump: %v", err)
	}
	if info.Engine != "mysql" {
		t.Fatalf("Engine = %q, want mysql", info.Engine)
	}
}
