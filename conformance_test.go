//go:build docker

package main

// Suíte de conformidade (SPEC.md, "Camada 2 — suíte de conformidade da
// Session"): um único corpo de teste, parametrizado pela lista de engines
// registradas (Engines()), sem nenhuma ramificação por nome de engine. Cada
// engine declara, no seu próprio arquivo de teste (ex.:
// postgres_conformance_test.go), como gerar seu backup de teste mínimo
// (válido e truncado) através de registerConformanceFixture, chamado de um
// init(). Uma engine registrada sem fixture correspondente falha aqui — a
// partir deste ticket, "a engine está pronta" significa exatamente "passa
// nesta suíte sem nenhuma exceção específica".
//
// Exige Docker, por isso fica atrás da build tag "docker" — a camada de
// detecção (detect_test.go), que concentra a lógica que mais quebra, continua
// rodando em qualquer máquina sem Docker.
//
// Roda com: go test -tags docker ./...

import (
	"context"
	"os"
	"strconv"
	"testing"
)

// ConformanceBackup é o que BuildValid devolve: o caminho do backup mínimo
// gerado (duas coleções — uma com linhas e uma vazia, uma com coluna de data
// e uma sem) e o oráculo contra o qual o corpo genérico compara o que a
// engine devolveu.
type ConformanceBackup struct {
	Path string
	// WantCollections é o conjunto exato de coleções esperadas, com a
	// contagem exata de cada uma (a coleção vazia entra com 0).
	WantCollections map[string]int64
	// DateCollection é o nome da coleção com coluna de data, usada para
	// verificar o limite de 20 linhas e a ordem decrescente de Recent.
	// Precisa ter mais de 20 linhas para exercitar o limite de verdade.
	DateCollection string
	// DateColumn é a coluna usada para ordenar DateCollection; os valores
	// que Recent devolve para ela precisam ordenar corretamente como string
	// (ex.: "AAAA-MM-DD[ HH:MM:SS]").
	DateColumn string
}

// ConformanceFixture é o que cada engine declara sobre como gerar seus
// backups de teste. O corpo de teste genérico abaixo só chama essas funções;
// toda lógica específica de engine (como gerar um dump, como corrompê-lo, o
// que é um comando nativo válido/inválido) fica contida na fixture.
type ConformanceFixture struct {
	// BuildValid gera o backup mínimo conhecido descrito no pacote.
	BuildValid func(t *testing.T) ConformanceBackup
	// BuildTruncated gera um backup deliberadamente truncado/corrompido, que
	// deve produzir erros de restore reportados, não engolidos.
	BuildTruncated func(t *testing.T) string
	// ValidQuery é um comando nativo que deve devolver resultado sem erro.
	ValidQuery string
	// InvalidQuery é um comando nativo malformado, que deve devolver erro
	// sem pânico.
	InvalidQuery string
}

var conformanceFixtures = map[string]ConformanceFixture{}

// registerConformanceFixture cadastra a fixture de conformidade de uma
// engine. Chamado do init() do arquivo de conformidade de cada engine (ex.:
// postgres_conformance_test.go).
func registerConformanceFixture(engine string, f ConformanceFixture) {
	conformanceFixtures[engine] = f
}

// TestEngineConformance é o corpo único de conformidade: roda contra toda
// engine registrada, sem nenhuma ramificação por nome de engine.
func TestEngineConformance(t *testing.T) {
	for _, eng := range Engines() {
		t.Run(eng.Name(), func(t *testing.T) {
			fx, ok := conformanceFixtures[eng.Name()]
			if !ok {
				t.Fatalf("engine %q registrada sem ConformanceFixture — registre uma via registerConformanceFixture antes de considerar a engine pronta", eng.Name())
			}
			requireDocker(t)

			t.Run("backup válido", func(t *testing.T) {
				testConformanceValid(t, eng, fx)
			})
			t.Run("backup truncado", func(t *testing.T) {
				testConformanceTruncated(t, eng, fx)
			})
		})
	}
}

func conformanceProvisionOpts() ProvisionOpts {
	return ProvisionOpts{Port: freePort(), Jobs: 4, DBName: "verify", ExactCounts: true}
}

// testConformanceValid provisiona o backup mínimo declarado pela fixture e
// verifica o contrato inteiro que toda engine precisa cumprir.
func testConformanceValid(t *testing.T, eng Engine, fx ConformanceFixture) {
	ctx := context.Background()
	cb := fx.BuildValid(t)
	if len(cb.WantCollections) == 0 {
		t.Fatal("fixture: BuildValid não declarou nenhuma coleção esperada em WantCollections")
	}

	backup, err := InspectDumpAs(cb.Path, eng.Name())
	if err != nil {
		t.Fatalf("InspectDumpAs: %v", err)
	}

	sess, err := eng.Provision(ctx, backup, conformanceProvisionOpts())
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	closed := false
	closeSession := func() error {
		if closed {
			return nil
		}
		closed = true
		return sess.Close()
	}
	t.Cleanup(func() { closeSession() })

	t.Run("restore de backup válido reporta zero erros", func(t *testing.T) {
		res := sess.Restore()
		if res == nil {
			t.Fatal("esperava RestoreResult não nulo")
		}
		if len(res.Errors) != 0 {
			t.Fatalf("esperava zero erros, tive %d: %v", len(res.Errors), res.Errors)
		}
	})

	t.Run("Health devolve nome e tamanho não vazios e nenhum contador negativo", func(t *testing.T) {
		health, err := sess.Health(ctx)
		if err != nil {
			t.Fatalf("Health: %v", err)
		}
		if health.Name == "" {
			t.Error("Health.Name veio vazio")
		}
		if health.Size == "" {
			t.Error("Health.Size veio vazio")
		}
		for _, f := range health.Fields {
			if n, err := strconv.Atoi(f.Value); err == nil && n < 0 {
				t.Errorf("campo %q veio negativo: %d", f.Label, n)
			}
		}
	})

	var collections []Collection
	t.Run("Collections devolve exatamente as coleções esperadas com contagem exata", func(t *testing.T) {
		var err error
		collections, err = sess.Collections(ctx, true) // contagem exata
		if err != nil {
			t.Fatalf("Collections: %v", err)
		}
		if len(collections) != len(cb.WantCollections) {
			t.Errorf("len(collections) = %d, want %d (veio %v)", len(collections), len(cb.WantCollections), collections)
		}
		for name, want := range cb.WantCollections {
			c, ok := collectionByName(collections, name)
			if !ok {
				t.Errorf("coleção %q não apareceu na listagem", name)
				continue
			}
			if c.Count != want {
				t.Errorf("%s: Count = %d, want %d", name, c.Count, want)
			}
		}
	})

	t.Run("Recent: no máximo 20 linhas, ordem decrescente na coleção com data", func(t *testing.T) {
		c, ok := collectionByName(collections, cb.DateCollection)
		if !ok {
			t.Fatalf("coleção com data %q não apareceu na listagem", cb.DateCollection)
		}
		rs, err := sess.Recent(ctx, c)
		if err != nil {
			t.Fatalf("Recent: %v", err)
		}
		if len(rs.Rows) == 0 {
			t.Fatal("Recent não devolveu nenhuma linha")
		}
		if len(rs.Rows) > 20 {
			t.Fatalf("len(rs.Rows) = %d, want <= 20", len(rs.Rows))
		}
		colIdx := -1
		for i, col := range rs.Columns {
			if col == cb.DateColumn {
				colIdx = i
			}
		}
		if colIdx < 0 {
			t.Fatalf("coluna %q não veio no resultado (colunas: %v)", cb.DateColumn, rs.Columns)
		}
		for i := 1; i < len(rs.Rows); i++ {
			prev, cur := rs.Rows[i-1][colIdx], rs.Rows[i][colIdx]
			if cur > prev {
				t.Fatalf("linha %d (%s) maior que a linha anterior (%s); esperava ordem decrescente", i, cur, prev)
			}
		}
	})

	t.Run("Query", func(t *testing.T) {
		t.Run("comando nativo válido devolve resultado", func(t *testing.T) {
			rs, err := sess.Query(ctx, fx.ValidQuery)
			if err != nil {
				t.Fatalf("Query(%q): %v", fx.ValidQuery, err)
			}
			if rs == nil {
				t.Fatal("Query devolveu resultado nulo")
			}
		})
		t.Run("comando nativo inválido devolve erro sem pânico", func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Query entrou em pânico com comando inválido: %v", r)
				}
			}()
			_, err := sess.Query(ctx, fx.InvalidQuery)
			if err == nil {
				t.Fatal("esperava erro para comando nativo inválido, veio nil")
			}
		})
	})

	t.Run("Close remove o container", func(t *testing.T) {
		hint := sess.ConnectHint()
		if hint.Name == "" {
			t.Skip("engine não expõe um container nomeado (ConnectHint.Name vazio)")
		}
		if !containerExists(hint.Name) {
			t.Fatal("container deveria existir antes do Close()")
		}
		if err := closeSession(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if containerExists(hint.Name) {
			t.Fatal("container ainda existe depois do Close()")
		}
	})
}

// testConformanceTruncated prova que erros de restore de um backup
// deliberadamente truncado são reportados, não engolidos.
func testConformanceTruncated(t *testing.T, eng Engine, fx ConformanceFixture) {
	ctx := context.Background()
	path := fx.BuildTruncated(t)

	backup, err := InspectDumpAs(path, eng.Name())
	if err != nil {
		t.Fatalf("InspectDumpAs: %v", err)
	}

	sess, err := eng.Provision(ctx, backup, conformanceProvisionOpts())
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	t.Cleanup(func() { sess.Close() })

	res := sess.Restore()
	if res == nil {
		t.Fatal("esperava RestoreResult não nulo")
	}
	if len(res.Errors) == 0 {
		t.Fatal("esperava erros de restore reportados para um backup truncado, veio zero")
	}
	if res.LogPath == "" {
		t.Fatal("esperava log do restore em arquivo (RestoreResult.LogPath vazio)")
	}
	if _, err := os.Stat(res.LogPath); err != nil {
		t.Fatalf("log do restore não encontrado em %q: %v", res.LogPath, err)
	}
}
