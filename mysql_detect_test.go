package main

// Detecção do MySQL contra as fixtures reais em testdata/headers: precisa do
// conjunto de engines montado na raiz, então não mora em engines/mysql.

import "testing"

// TestMySQLDetect_Header caracteriza a detecção pelo cabeçalho de texto do
// mysqldump: magic bytes, versão (preferindo "Server version", caindo para
// "Distrib" quando ausente) e banco de origem.
func TestMySQLDetect_Header(t *testing.T) {
	cases := []struct {
		name        string
		path        string
		wantVersion string
		wantOrigin  string
	}{
		{"com Server version", "testdata/headers/mysql.sql", "8.0", "fixturedb"},
		{"só Distrib", "testdata/headers/mysql-distrib-only.sql", "5.7", "legacydb"},
		{"sem nenhuma versão", "testdata/headers/mysql-no-version.sql", "", "outrodb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := InspectDump(tc.path)
			if err != nil {
				t.Fatalf("InspectDump(%q): %v", tc.path, err)
			}
			if info.Engine != "mysql" {
				t.Fatalf("Engine = %q, want mysql", info.Engine)
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

// TestMySQLDetect_Gzip caracteriza a detecção através de gzip: o cabeçalho é
// descomprimido antes de a engine olhar para ele, então o mysqldump gzipado
// é reconhecido igual ao plano.
func TestMySQLDetect_Gzip(t *testing.T) {
	info, err := InspectDump("testdata/headers/mysql.sql.gz")
	if err != nil {
		t.Fatalf("InspectDump: %v", err)
	}
	if info.Compression != "gzip" {
		t.Errorf("Compression = %q, want gzip", info.Compression)
	}
	if info.Engine != "mysql" {
		t.Errorf("Engine = %q, want mysql", info.Engine)
	}
	if info.Version != "8.0" {
		t.Errorf("Version = %q, want 8.0", info.Version)
	}
}
