package main

// Testes de caracterização de InspectDump: descrevem o que o db-verify faz
// hoje, contra fixtures reais de cabeçalho versionadas em testdata/headers/.
// Nenhum destes testes deve ser reescrito pela extração da interface Engine —
// se algum precisar mudar, ou houve regressão de verdade, ou o teste estava
// amarrado à forma interna (ver SPEC.md, "Camada 0").

import "testing"

// TestInspectDump_Formatos cobre a detecção de formato/versão/banco de
// origem para os três formatos de dump que o Postgres produz hoje: custom
// (PGDMP), plain SQL e tar. As fixtures são cabeçalhos reais truncados,
// gerados a partir de um pg_dump de verdade contra um Postgres 16.
func TestInspectDump_Formatos(t *testing.T) {
	cases := []struct {
		name        string
		path        string
		wantFormat  string
		wantCompr   string
		wantOrigin  string
		wantVersion string
	}{
		{
			name:        "custom PGDMP",
			path:        "testdata/headers/custom.dump",
			wantFormat:  "custom",
			wantCompr:   "none",
			wantOrigin:  "fixturedb",
			wantVersion: "16",
		},
		{
			name:        "plain SQL",
			path:        "testdata/headers/plain.sql",
			wantFormat:  "plain",
			wantCompr:   "none",
			wantOrigin:  "fixturedb",
			wantVersion: "16",
		},
		{
			// Hoje a detecção de tar não olha o conteúdo: depende só do
			// sufixo ".tar" no caminho (ver dump.go, InspectDump). Por isso
			// não extrai OriginDB nem Version — comportamento atual, não
			// ideal, mas é o que este teste trava.
			name:        "tar por extensão",
			path:        "testdata/headers/tar.tar",
			wantFormat:  "tar",
			wantCompr:   "none",
			wantOrigin:  "",
			wantVersion: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := InspectDump(tc.path)
			if err != nil {
				t.Fatalf("InspectDump(%q) erro inesperado: %v", tc.path, err)
			}
			if info.Format != tc.wantFormat {
				t.Errorf("Format = %q, want %q", info.Format, tc.wantFormat)
			}
			if info.Compression != tc.wantCompr {
				t.Errorf("Compression = %q, want %q", info.Compression, tc.wantCompr)
			}
			if info.OriginDB != tc.wantOrigin {
				t.Errorf("OriginDB = %q, want %q", info.OriginDB, tc.wantOrigin)
			}
			if info.Version != tc.wantVersion {
				t.Errorf("Version = %q, want %q", info.Version, tc.wantVersion)
			}
		})
	}
}

// TestInspectDump_Gzip caracteriza a detecção quando cada fixture acima é
// gzipada. custom e plain são detectados pelo conteúdo (que é descomprimido
// antes de inspecionar o cabeçalho), então continuam corretos. tar é
// detectado só pelo sufixo do caminho original — "tar.tar.gz" não termina em
// ".tar", então a detecção de tar não sobrevive ao gzip hoje e cai no
// palpite padrão ("plain"). Este é o comportamento atual do código, travado
// aqui de propósito para não regredir silenciosamente durante a extração da
// interface Engine.
func TestInspectDump_Gzip(t *testing.T) {
	cases := []struct {
		name        string
		path        string
		wantFormat  string
		wantOrigin  string
		wantVersion string
	}{
		{
			name:        "custom PGDMP gzipado",
			path:        "testdata/headers/custom.dump.gz",
			wantFormat:  "custom",
			wantOrigin:  "fixturedb",
			wantVersion: "16",
		},
		{
			name:        "plain SQL gzipado",
			path:        "testdata/headers/plain.sql.gz",
			wantFormat:  "plain",
			wantOrigin:  "fixturedb",
			wantVersion: "16",
		},
		{
			name:        "tar gzipado não é reconhecido como tar",
			path:        "testdata/headers/tar.tar.gz",
			wantFormat:  "plain", // não "tar" -- ver comentário acima
			wantOrigin:  "",
			wantVersion: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := InspectDump(tc.path)
			if err != nil {
				t.Fatalf("InspectDump(%q) erro inesperado: %v", tc.path, err)
			}
			if info.Compression != "gzip" {
				t.Errorf("Compression = %q, want gzip", info.Compression)
			}
			if info.Format != tc.wantFormat {
				t.Errorf("Format = %q, want %q", info.Format, tc.wantFormat)
			}
			if info.OriginDB != tc.wantOrigin {
				t.Errorf("OriginDB = %q, want %q", info.OriginDB, tc.wantOrigin)
			}
			if info.Version != tc.wantVersion {
				t.Errorf("Version = %q, want %q", info.Version, tc.wantVersion)
			}
		})
	}
}

// TestInspectDump_ArquivoVazio caracteriza o comportamento hoje para um
// arquivo de tamanho zero: não há erro, e a falta de qualquer assinatura
// reconhecida cai no palpite padrão do switch em InspectDump ("plain").
func TestInspectDump_ArquivoVazio(t *testing.T) {
	info, err := InspectDump("testdata/headers/empty.dump")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if info.Size != 0 {
		t.Errorf("Size = %d, want 0", info.Size)
	}
	if info.Format != "plain" {
		t.Errorf("Format = %q, want %q (palpite padrão)", info.Format, "plain")
	}
	if info.OriginDB != "" || info.Version != "" {
		t.Errorf("esperava OriginDB/Version vazios, tive %q/%q", info.OriginDB, info.Version)
	}
}

// TestInspectDump_TextoAleatorio caracteriza o comportamento hoje para um
// arquivo de texto sem qualquer assinatura de dump Postgres: também cai no
// palpite padrão "plain", sem erro — a "ambiguidade conhecida" descrita na
// SPEC (um .sql sem cabeçalho reconhecível é atribuído a Postgres).
func TestInspectDump_TextoAleatorio(t *testing.T) {
	info, err := InspectDump("testdata/headers/random.txt")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if info.Format != "plain" {
		t.Errorf("Format = %q, want %q (palpite padrão)", info.Format, "plain")
	}
	if info.OriginDB != "" || info.Version != "" {
		t.Errorf("esperava OriginDB/Version vazios, tive %q/%q", info.OriginDB, info.Version)
	}
}
