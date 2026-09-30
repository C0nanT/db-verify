package main

// Detecção do SQLite contra as fixtures reais em testdata/headers: precisa do
// conjunto de engines montado na raiz, então não mora em engines/sqlite.

import "testing"

// TestSQLiteDetect_Header e TestSQLiteDetect_Gzip caracterizam a detecção
// via InspectDump usando as fixtures reais em testdata/headers, incluindo
// comprimida — mesmo padrão das demais engines (ver mysql_detect_test.go).
func TestSQLiteDetect_Header(t *testing.T) {
	info, err := InspectDump("testdata/headers/sqlite.db")
	if err != nil {
		t.Fatalf("InspectDump: %v", err)
	}
	if info.Engine != "sqlite" {
		t.Fatalf("Engine = %q, want sqlite", info.Engine)
	}
	if info.Guessed {
		t.Errorf("esperava Guessed=false para cabeçalho reconhecido por magic bytes")
	}
}

func TestSQLiteDetect_Gzip(t *testing.T) {
	info, err := InspectDump("testdata/headers/sqlite.db.gz")
	if err != nil {
		t.Fatalf("InspectDump: %v", err)
	}
	if info.Compression != "gzip" {
		t.Errorf("Compression = %q, want gzip", info.Compression)
	}
	if info.Engine != "sqlite" {
		t.Errorf("Engine = %q, want sqlite", info.Engine)
	}
}
