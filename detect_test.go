package main

// Testes de detecção (Camada 1 da SPEC): descompressão de cabeçalho
// (gzip/zstd/bzip2), a disputa entre engines por confiança (magic bytes >
// extensão > palpite, empate por ordem de registro), o erro de "nenhuma
// engine reconheceu" e o comportamento de --engine.
//
// Os testes de disputa/erro usam engines fictícias (fakeEngine, abaixo) em
// vez do registro global: com só Postgres registrado — que tem um palpite
// que reconhece qualquer coisa (ver postgres.go, Detect, caso default) —
// esses cenários nunca ocorreriam de ponta a ponta. chooseEngine e
// noEngineErr existem justamente para serem exercitados isoladamente aqui,
// sem depender de uma segunda engine de verdade estar registrada.

import (
	"context"
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

// fakeEngine é uma engine mínima para testar a disputa de confiança sem
// depender do registro global nem de uma engine de verdade.
type fakeEngine struct {
	name    string
	detect  func(head []byte, path string) (Match, bool)
	expects string
}

func (f fakeEngine) Name() string { return f.name }
func (f fakeEngine) Detect(head []byte, path string) (Match, bool) {
	if f.detect == nil {
		return Match{}, false
	}
	return f.detect(head, path)
}
func (f fakeEngine) Expects() string { return f.expects }
func (f fakeEngine) Provision(ctx context.Context, b *Backup, opts ProvisionOpts) (Session, error) {
	panic("fakeEngine.Provision não deveria ser chamado em teste de detecção")
}

func matchAlways(format string, confidence int) func([]byte, string) (Match, bool) {
	return func(head []byte, path string) (Match, bool) {
		return Match{Format: format, Confidence: confidence}, true
	}
}

func matchNever(head []byte, path string) (Match, bool) { return Match{}, false }

// TestChooseEngine_MagicVenceExtensaoVencePalpite caracteriza o desempate
// por confiança: magic bytes (100) > extensão (50) > palpite (10),
// independentemente da ordem em que as engines estão na lista.
func TestChooseEngine_MagicVenceExtensaoVencePalpite(t *testing.T) {
	magic := fakeEngine{name: "magic", detect: matchAlways("m", ConfidenceMagic)}
	ext := fakeEngine{name: "ext", detect: matchAlways("e", ConfidenceExtension)}
	guess := fakeEngine{name: "guess", detect: matchAlways("g", ConfidenceGuess)}

	eng, m, ok := chooseEngine([]Engine{guess, ext, magic}, nil, "arquivo")
	if !ok || eng.Name() != "magic" || m.Format != "m" {
		t.Fatalf("esperava magic vencer, tive engine=%v format=%q ok=%v", eng, m.Format, ok)
	}

	eng, _, ok = chooseEngine([]Engine{guess, magic, ext}, nil, "arquivo")
	if !ok || eng.Name() != "magic" {
		t.Fatalf("esperava magic vencer independente da ordem, tive %v", eng)
	}

	eng, _, ok = chooseEngine([]Engine{guess, ext}, nil, "arquivo")
	if !ok || eng.Name() != "ext" {
		t.Fatalf("esperava extensão vencer palpite, tive %v", eng)
	}
}

// TestChooseEngine_EmpateResolvePorOrdemDeRegistro: duas engines com a
// mesma confiança — a primeira da lista (ordem de registro) vence.
func TestChooseEngine_EmpateResolvePorOrdemDeRegistro(t *testing.T) {
	first := fakeEngine{name: "primeira", detect: matchAlways("f", ConfidenceMagic)}
	second := fakeEngine{name: "segunda", detect: matchAlways("s", ConfidenceMagic)}

	eng, _, ok := chooseEngine([]Engine{first, second}, nil, "arquivo")
	if !ok || eng.Name() != "primeira" {
		t.Fatalf("esperava a primeira registrada vencer o empate, tive %v", eng)
	}

	// invertendo a ordem de registro, a vencedora muda junto.
	eng, _, ok = chooseEngine([]Engine{second, first}, nil, "arquivo")
	if !ok || eng.Name() != "segunda" {
		t.Fatalf("esperava a primeira da lista (agora segunda) vencer, tive %v", eng)
	}
}

// TestChooseEngine_NenhumaReconhece: quando nenhuma engine da lista
// reconhece o cabeçalho, chooseEngine devolve ok=false, e noEngineErr monta
// um erro nomeando as engines disponíveis e o que cada uma espera.
func TestChooseEngine_NenhumaReconhece(t *testing.T) {
	a := fakeEngine{name: "aa", detect: matchNever, expects: "cabeçalho AA"}
	b := fakeEngine{name: "bb", detect: matchNever, expects: "cabeçalho BB"}

	_, _, ok := chooseEngine([]Engine{a, b}, []byte("lixo"), "arquivo.bin")
	if ok {
		t.Fatalf("esperava ok=false quando nenhuma engine reconhece")
	}

	err := noEngineErr([]Engine{a, b})
	if err == nil {
		t.Fatalf("esperava erro não nulo")
	}
	msg := err.Error()
	for _, want := range []string{"aa", "cabeçalho AA", "bb", "cabeçalho BB"} {
		if !strings.Contains(msg, want) {
			t.Errorf("mensagem de erro não menciona %q: %s", want, msg)
		}
	}
}

// TestInspectDumpAs_ForcaEngineESepulaDisputa: com --engine, só a engine
// forçada é consultada — mesmo que ela não reconheça o conteúdo, o
// resultado usa o que ela conseguiu extrair (aqui, nada) em vez de cair
// noutra engine ou falhar.
func TestInspectDumpAs_ForcaEngineESepulaDisputa(t *testing.T) {
	info, err := InspectDumpAs("testdata/headers/plain.sql", "postgres")
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
	_, err := InspectDumpAs("testdata/headers/plain.sql", "oracle")
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
	info, err := InspectDump("testdata/headers/random.txt")
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
	info, err := InspectDump("testdata/headers/custom.dump")
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
			info, err := InspectDump(tc.path)
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
			info, err := InspectDump(tc.path)
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
		_, err := InspectDump(f)
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
