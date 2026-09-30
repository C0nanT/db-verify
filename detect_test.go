package main

// Testes de detecção (Camada 1 da SPEC): descompressão de cabeçalho
// (gzip/zstd/bzip2), a disputa entre engines por confiança (magic bytes >
// extensão > palpite, empate por ordem de registro), o erro de "nenhuma
// engine reconheceu" e o comportamento de --engine.
//
// Aqui ficam os testes que precisam das engines reais registradas (a lista
// de registrarEngines). A disputa de confiança e o erro de "nenhuma engine
// reconheceu", testados com engines fictícias, vivem em internal/detect.

import (
	"db-verify/internal/detect"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// createSparseFile cria, no diretório temporário do teste, um arquivo que
// começa com header e tem size bytes no total, sem escrever (nem ocupar
// disco para) os bytes depois do cabeçalho — um arquivo esparso, como um
// dump gigante de verdade seria lido preguiçosamente.
func createSparseFile(t *testing.T, header string, size int64) (string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "enorme.dump")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(header); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		return "", err
	}
	return path, f.Close()
}

// TestInspectDumpAs_ForcaEngineESepulaDisputa: com --engine, só a engine
// forçada é consultada — mesmo que ela não reconheça o conteúdo, o
// resultado usa o que ela conseguiu extrair (aqui, nada) em vez de cair
// noutra engine ou falhar.
func TestInspectDumpAs_ForcaEngineESepulaDisputa(t *testing.T) {
	info, err := detect.InspectDumpAs("testdata/headers/plain.sql", "postgres")
	if err != nil {
		t.Fatalf("InspectDumpAs: %v", err)
	}
	if info.Engine != "postgres" {
		t.Errorf("Engine = %q, want postgres", info.Engine)
	}
	if !info.Forced {
		t.Errorf("esperava Forced=true")
	}
}

// TestInspectDumpAs_EngineInexistente: --engine com nome que não está
// registrado falha com um erro que lista as engines disponíveis.
func TestInspectDumpAs_EngineInexistente(t *testing.T) {
	_, err := detect.InspectDumpAs("testdata/headers/plain.sql", "oracle")
	if err == nil {
		t.Fatalf("esperava erro para engine inexistente")
	}
	if !strings.Contains(err.Error(), "oracle") || !strings.Contains(err.Error(), "postgres") {
		t.Errorf("erro deveria nomear a engine pedida e as disponíveis: %v", err)
	}
}

// TestInspectDump_PlainSQLAmbiguoCaiEmPostgresComPalpite caracteriza a
// ambiguidade conhecida (SPEC.md): um .sql sem cabeçalho identificável cai
// em Postgres por palpite, e o resultado é marcado como Guessed para o
// chamador (main.go) avisar e sugerir --engine.
func TestInspectDump_PlainSQLAmbiguoCaiEmPostgresComPalpite(t *testing.T) {
	info, err := detect.InspectDump("testdata/headers/random.txt")
	if err != nil {
		t.Fatalf("InspectDump: %v", err)
	}
	if !info.Guessed {
		t.Errorf("esperava Guessed=true para arquivo sem assinatura reconhecível")
	}
	if info.Forced {
		t.Errorf("Forced não deveria estar marcado sem --engine")
	}
}

// TestInspectDump_MagicNaoEhPalpite: um cabeçalho reconhecido por magic
// bytes não deve ser marcado como Guessed.
func TestInspectDump_MagicNaoEhPalpite(t *testing.T) {
	info, err := detect.InspectDump("testdata/headers/custom.dump")
	if err != nil {
		t.Fatalf("InspectDump: %v", err)
	}
	if info.Guessed {
		t.Errorf("PGDMP é magic byte, não deveria ser Guessed")
	}
}

// TestInspectDump_Zstd e TestInspectDump_Bzip2 caracterizam a descompressão
// de cabeçalho através de zstd e bzip2 (além de gzip, já coberto em
// dump_test.go), usando as mesmas fixtures reais comprimidas com cada
// formato (ver testdata/headers).
func TestInspectDump_Zstd(t *testing.T) {
	cases := []struct {
		path       string
		wantFormat string
		wantOrigin string
	}{
		{"testdata/headers/custom.dump.zst", "custom", "fixturedb"},
		{"testdata/headers/plain.sql.zst", "plain", "fixturedb"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			info, err := detect.InspectDump(tc.path)
			if err != nil {
				t.Fatalf("InspectDump(%q): %v", tc.path, err)
			}
			if info.Compression != "zstd" {
				t.Errorf("Compression = %q, want zstd", info.Compression)
			}
			if info.Format != tc.wantFormat {
				t.Errorf("Format = %q, want %q", info.Format, tc.wantFormat)
			}
			if info.OriginDB != tc.wantOrigin {
				t.Errorf("OriginDB = %q, want %q", info.OriginDB, tc.wantOrigin)
			}
		})
	}
}

func TestInspectDump_Bzip2(t *testing.T) {
	cases := []struct {
		path       string
		wantFormat string
		wantOrigin string
	}{
		{"testdata/headers/custom.dump.bz2", "custom", "fixturedb"},
		{"testdata/headers/plain.sql.bz2", "plain", "fixturedb"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			info, err := detect.InspectDump(tc.path)
			if err != nil {
				t.Fatalf("InspectDump(%q): %v", tc.path, err)
			}
			if info.Compression != "bzip2" {
				t.Errorf("Compression = %q, want bzip2", info.Compression)
			}
			if info.Format != tc.wantFormat {
				t.Errorf("Format = %q, want %q", info.Format, tc.wantFormat)
			}
			if info.OriginDB != tc.wantOrigin {
				t.Errorf("OriginDB = %q, want %q", info.OriginDB, tc.wantOrigin)
			}
		})
	}
}

// TestInspectDump_ArquivoEnormeNaoTrava garante que a detecção só lê o
// cabeçalho: um arquivo esparso de vários gigabytes (que não ocupa disco de
// verdade) precisa ser inspecionado rapidamente, não lido inteiro.
func TestInspectDump_ArquivoEnormeNaoTrava(t *testing.T) {
	f, err := createSparseFile(t, "PGDMP\x00\x00\x00mais dados de cabeçalho aqui", 8<<30) // 8 GiB esparso
	if err != nil {
		t.Fatalf("criando arquivo esparso: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := detect.InspectDump(f)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("InspectDump: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("InspectDump não terminou em 5s — parece estar lendo o arquivo inteiro")
	}
}
