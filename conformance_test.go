//go:build docker

package main

// Suíte de conformidade da Session: um único corpo de teste, parametrizado
// pela lista de engines registradas (Engines()), sem nenhuma ramificação por
// nome de engine. Cada engine declara como gerar seu backup de teste mínimo
// (válido e truncado) cadastrando uma conformance.ConformanceFixture via
// conformance.Register, chamado de um init(). Uma engine registrada sem
// fixture correspondente falha aqui — "a engine está pronta" significa
// exatamente "passa nesta suíte sem nenhuma exceção específica".
//
// Exige Docker, por isso fica atrás da build tag "docker" — a camada de
// detecção, que concentra a lógica que mais quebra, continua rodando em
// qualquer máquina sem Docker.
//
// Roda com: go test -tags docker ./...

import (
	"context"
	"db-verify/internal/detect"
	"db-verify/internal/engine"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"

	"db-verify/internal/conformance"
)

// TestEngineConformance é o corpo único de conformidade: roda contra toda
// engine registrada, sem nenhuma ramificação por nome de engine.
func TestEngineConformance(t *testing.T) {
	for _, eng := range engine.Engines() {
		t.Run(eng.Name(), func(t *testing.T) {
			fx, ok := conformance.Lookup(eng.Name())
			if !ok {
				t.Fatalf("engine %q registrada sem ConformanceFixture — registre uma via conformance.Register antes de considerar a engine pronta", eng.Name())
			}
			conformance.RequireDocker(t)

			t.Run("backup válido", func(t *testing.T) {
				testConformanceValid(t, eng, fx)
			})
			t.Run("backup truncado", func(t *testing.T) {
				testConformanceTruncated(t, eng, fx)
			})
			t.Run("cancelamento em cada passo", func(t *testing.T) {
				testConformanceCancel(t, eng, fx)
			})
		})
	}
}

func conformanceProvisionOpts() engine.ProvisionOpts {
	// Port 0 = mesmo que omitir --port: cada engine usa a própria janela
	// (Postgres 55432, MySQL/MariaDB 3306, Redis 6379, Mongo 27017).
	return engine.ProvisionOpts{Jobs: 4, DBName: "verify", ExactCounts: true}
}

// testConformanceValid provisiona o backup mínimo declarado pela fixture e
// verifica o contrato inteiro que toda engine precisa cumprir.
func testConformanceValid(t *testing.T, eng engine.Engine, fx conformance.ConformanceFixture) {
	ctx := context.Background()
	cb := fx.BuildValid(t)
	if len(cb.WantCollections) == 0 {
		t.Fatal("fixture: BuildValid não declarou nenhuma coleção esperada em WantCollections")
	}

	backup, err := detect.InspectDumpAs(cb.Path, eng.Name())
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

	var collections []engine.Collection
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
			c, ok := conformance.CollectionByName(collections, name)
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
		c, ok := conformance.CollectionByName(collections, cb.DateCollection)
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
		if !conformance.ContainerExists(hint.Name) {
			t.Fatal("container deveria existir antes do Close()")
		}
		if err := closeSession(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if conformance.ContainerExists(hint.Name) {
			t.Fatal("container ainda existe depois do Close()")
		}
	})
}

// testConformanceTruncated prova que erros de restore de um backup
// deliberadamente truncado são reportados, não engolidos.
func testConformanceTruncated(t *testing.T, eng engine.Engine, fx conformance.ConformanceFixture) {
	ctx := context.Background()
	path := fx.BuildTruncated(t)

	backup, err := detect.InspectDumpAs(path, eng.Name())
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

// testConformanceCancel prova o contrato de Provision sob cancelamento: mede
// n, o número de relatórios de Progress num Provision bem-sucedido, e para
// cada k em 1..n cancela o ctx no k-ésimo relatório. Em todos os casos o
// Provision devolve erro (nunca Session) e não resta container
// db-verify-<pid>, nem parado. Engines sem container passam trivialmente
// na segunda checagem, mas não na primeira.
func testConformanceCancel(t *testing.T, eng engine.Engine, fx conformance.ConformanceFixture) {
	cb := fx.BuildValid(t)
	backup, err := detect.InspectDumpAs(cb.Path, eng.Name())
	if err != nil {
		t.Fatalf("InspectDumpAs: %v", err)
	}

	// Todas as engines usam o mesmo nome; um vazamento aqui faria os
	// Provision seguintes falharem por nome em uso, então sempre removemos.
	name := fmt.Sprintf("db-verify-%d", os.Getpid())
	forceRemove := func() { _ = exec.Command("docker", "rm", "-f", name).Run() }
	t.Cleanup(forceRemove)
	requireNoContainer := func(t *testing.T, when string) {
		t.Helper()
		// conformance.ContainerExists usa `docker inspect`, que encontra o container em
		// qualquer estado (Created/Exited inclusive), não só rodando.
		if conformance.ContainerExists(name) {
			forceRemove()
			t.Fatalf("container %s existe %s", name, when)
		}
	}

	requireNoContainer(t, "antes de medir os passos")
	n := 0
	opts := conformanceProvisionOpts()
	opts.Progress = func(string, ...any) { n++ }
	sess, err := eng.Provision(context.Background(), backup, opts)
	if err != nil {
		t.Fatalf("Provision (medindo os passos): %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close (medindo os passos): %v", err)
	}
	if n == 0 {
		t.Fatal("Provision não reportou nenhum progresso; não há passo onde cancelar")
	}

	for k := 1; k <= n; k++ {
		t.Run(fmt.Sprintf("cancela no relatório %d de %d", k, n), func(t *testing.T) {
			requireNoContainer(t, "antes do Provision")

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			opts := conformanceProvisionOpts()
			opts.Progress = func(string, ...any) {
				calls++
				if calls == k {
					cancel()
				}
			}
			sess, err := eng.Provision(ctx, backup, opts)
			if sess != nil {
				sess.Close()
				t.Fatalf("Provision cancelado devolveu Session (err=%v)", err)
			}
			if err == nil {
				t.Fatal("Provision cancelado devolveu erro nil")
			}
			requireNoContainer(t, "depois do Provision cancelado")
		})
	}
}
