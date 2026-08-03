//go:build docker

package main

// Teste de fluxo completo (Camada 0 / regra "fluxo completo exige Docker").
// Gera um dump Postgres pequeno com o próprio teste (não versionado, não
// depende de nada em data/), restaura através da engine Postgres tal como
// ela existe hoje atrás da interface Engine/Session (Lookup, Provision,
// Restore, Health, Collections, Recent) e caracteriza: restore sem erros,
// listagem de tabelas com contagem exata (incluindo tabela vazia não
// omitida), a heurística de coluna de ordenação para cada família suportada
// hoje e o SQL resultante de cada uma, o limite de 20 linhas em ordem
// decrescente, e o ciclo de vida do container (removido ao encerrar,
// preservado quando simulamos --keep).
//
// Este arquivo era originalmente escrito contra Container/Connect/
// FetchHealth/FetchTables/RunQuery diretamente; a extração da interface
// Engine/Session (ticket 02) moveu esse fluxo para trás de Provision/Session
// (Container virou pgContainer, FetchTables virou Session.Collections,
// etc.) — as chamadas foram adaptadas, as asserções (valores esperados)
// continuam as mesmas.
//
// Roda com: go test -tags docker ./...

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const testSchemaSQL = `
CREATE TABLE tbl_created (
    id serial PRIMARY KEY,
    created_at timestamp NOT NULL
);
CREATE TABLE tbl_data_criacao (
    id serial PRIMARY KEY,
    data_criacao date NOT NULL
);
CREATE TABLE tbl_published (
    id serial PRIMARY KEY,
    published_at timestamp NOT NULL
);
CREATE TABLE tbl_data (
    id serial PRIMARY KEY,
    data timestamp NOT NULL
);
CREATE TABLE tbl_updated (
    id serial PRIMARY KEY,
    updated_at timestamp NOT NULL
);
CREATE TABLE tbl_generic_ts (
    id serial PRIMARY KEY,
    some_moment timestamp NOT NULL
);
CREATE TABLE tbl_pk_only (
    id serial PRIMARY KEY,
    label text NOT NULL
);
CREATE TABLE tbl_no_option (
    label text NOT NULL,
    other text NOT NULL
);
CREATE TABLE tbl_empty (
    id serial PRIMARY KEY,
    created_at timestamp NOT NULL
);

INSERT INTO tbl_created (created_at)
  SELECT timestamp '2024-01-01' + (g * interval '1 day')
  FROM generate_series(1, 25) AS g;

INSERT INTO tbl_data_criacao (data_criacao) VALUES
  ('2024-01-01'), ('2024-01-02'), ('2024-01-03');

INSERT INTO tbl_published (published_at) VALUES
  ('2024-01-01 10:00'), ('2024-01-02 10:00');

INSERT INTO tbl_data (data) VALUES
  ('2024-01-01 10:00'), ('2024-01-02 10:00');

INSERT INTO tbl_updated (updated_at) VALUES
  ('2024-01-01 10:00'), ('2024-01-02 10:00');

INSERT INTO tbl_generic_ts (some_moment) VALUES
  ('2024-01-01 10:00'), ('2024-01-02 10:00');

INSERT INTO tbl_pk_only (label) VALUES
  ('a'), ('b');

INSERT INTO tbl_no_option (label, other) VALUES
  ('a', 'x'), ('b', 'y');
`

func requireDocker(t *testing.T) {
	t.Helper()
	if err := dockerAvailable(); err != nil {
		t.Skipf("docker indisponível: %v", err)
	}
}

func containerExists(name string) bool {
	return exec.Command("docker", "inspect", name).Run() == nil
}

func uniqueName(part string) string {
	return fmt.Sprintf("db-verify-test-%s-%d-%d", part, os.Getpid(), time.Now().UnixNano())
}

// buildSourceDump sobe um Postgres "de origem" descartável, aplica o schema
// de teste e devolve o caminho de um dump em formato custom gerado por
// pg_dump dentro do próprio container (mesma versão de servidor e cliente).
// O container de origem é removido ao final do teste; o dump gerado fica
// num diretório temporário do teste (não entra no repo).
func buildSourceDump(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	srcName := uniqueName("src")
	out, err := exec.Command("docker", "run", "-d", "--name", srcName,
		"-e", "POSTGRES_PASSWORD=postgres",
		"-e", "POSTGRES_USER=postgres",
		"-e", "POSTGRES_DB=srcdb",
		"postgres:16-alpine").CombinedOutput()
	if err != nil {
		t.Fatalf("falha ao subir container de origem: %s", strings.TrimSpace(string(out)))
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", srcName).Run() })

	deadline := time.Now().Add(60 * time.Second)
	for {
		if exec.Command("docker", "exec", srcName, "pg_isready", "-U", "postgres", "-d", "srcdb").Run() == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout esperando o Postgres de origem ficar pronto")
		}
		time.Sleep(500 * time.Millisecond)
	}

	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", srcName,
		"psql", "-U", "postgres", "-d", "srcdb", "-v", "ON_ERROR_STOP=1")
	cmd.Stdin = strings.NewReader(testSchemaSQL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("falha ao aplicar schema de teste: %s", strings.TrimSpace(string(out)))
	}

	if out, err := exec.Command("docker", "exec", srcName,
		"pg_dump", "-U", "postgres", "-d", "srcdb", "-Fc", "-f", "/tmp/test.dump").CombinedOutput(); err != nil {
		t.Fatalf("pg_dump falhou: %s", strings.TrimSpace(string(out)))
	}

	local := t.TempDir() + "/test.dump"
	if out, err := exec.Command("docker", "cp", srcName+":/tmp/test.dump", local).CombinedOutput(); err != nil {
		t.Fatalf("docker cp falhou: %s", strings.TrimSpace(string(out)))
	}
	return local
}

// collectionByName é um helper de busca linear — a lista de Collections não
// é grande o bastante para justificar um índice.
func collectionByName(collections []Collection, name string) (Collection, bool) {
	for _, c := range collections {
		if c.Name == name {
			return c, true
		}
	}
	return Collection{}, false
}

func TestFullFlow_Postgres(t *testing.T) {
	requireDocker(t)

	dumpPath := buildSourceDump(t)

	backup, err := InspectDump(dumpPath)
	if err != nil {
		t.Fatalf("InspectDump: %v", err)
	}
	if backup.Format != "custom" {
		t.Fatalf("Format = %q, want custom", backup.Format)
	}

	eng, ok := Lookup("postgres")
	if !ok {
		t.Fatal(`engine "postgres" não registrada`)
	}

	ctx := context.Background()
	sess, err := eng.Provision(ctx, backup, ProvisionOpts{
		Port: freePort(), Jobs: 4, DBName: "verify", ExactCounts: true,
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	t.Cleanup(func() { sess.Close() })

	res := sess.Restore()
	t.Run("restore sem erros", func(t *testing.T) {
		if res == nil {
			t.Fatal("esperava RestoreResult não nulo")
		}
		if len(res.Errors) != 0 {
			t.Fatalf("esperava zero erros, tive %d: %v", len(res.Errors), res.Errors)
		}
	})

	collections, err := sess.Collections(ctx, true) // contagem exata
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}

	t.Run("listagem devolve as tabelas esperadas com contagem exata", func(t *testing.T) {
		want := map[string]int64{
			"tbl_created":      25,
			"tbl_data_criacao": 3,
			"tbl_published":    2,
			"tbl_data":         2,
			"tbl_updated":      2,
			"tbl_generic_ts":   2,
			"tbl_pk_only":      2,
			"tbl_no_option":    2,
			"tbl_empty":        0,
		}
		for name, wantRows := range want {
			c, ok := collectionByName(collections, name)
			if !ok {
				t.Errorf("tabela %q não apareceu na listagem", name)
				continue
			}
			if c.Count != wantRows {
				t.Errorf("%s: Count = %d, want %d", name, c.Count, wantRows)
			}
		}
	})

	t.Run("tabela vazia aparece com contagem zero, não é omitida", func(t *testing.T) {
		c, ok := collectionByName(collections, "tbl_empty")
		if !ok {
			t.Fatal("tbl_empty não apareceu na listagem")
		}
		if c.Count != 0 {
			t.Errorf("Count = %d, want 0", c.Count)
		}
	})

	t.Run("heurística de coluna de ordenação por família", func(t *testing.T) {
		cases := []struct {
			table      string
			wantCol    string
			wantByDate bool
			wantSQL    string
		}{
			{"tbl_created", "created_at", true,
				`SELECT * FROM "public"."tbl_created" ORDER BY "created_at" DESC LIMIT 20;`},
			{"tbl_data_criacao", "data_criacao", true,
				`SELECT * FROM "public"."tbl_data_criacao" ORDER BY "data_criacao" DESC LIMIT 20;`},
			{"tbl_published", "published_at", true,
				`SELECT * FROM "public"."tbl_published" ORDER BY "published_at" DESC LIMIT 20;`},
			{"tbl_data", "data", true,
				`SELECT * FROM "public"."tbl_data" ORDER BY "data" DESC LIMIT 20;`},
			{"tbl_updated", "updated_at", true,
				`SELECT * FROM "public"."tbl_updated" ORDER BY "updated_at" DESC LIMIT 20;`},
			{"tbl_generic_ts", "some_moment", true,
				`SELECT * FROM "public"."tbl_generic_ts" ORDER BY "some_moment" DESC LIMIT 20;`},
			{"tbl_pk_only", "id", false,
				`SELECT * FROM "public"."tbl_pk_only" ORDER BY "id" DESC LIMIT 20;`},
			{"tbl_no_option", "", false,
				`SELECT * FROM "public"."tbl_no_option" LIMIT 20;`},
		}
		for _, tc := range cases {
			t.Run(tc.table, func(t *testing.T) {
				c, ok := collectionByName(collections, tc.table)
				if !ok {
					t.Fatalf("tabela %q não apareceu na listagem", tc.table)
				}
				d, ok := c.Descriptor.(pgDescriptor)
				if !ok {
					t.Fatalf("Descriptor não é pgDescriptor: %#v", c.Descriptor)
				}
				if d.OrderCol != tc.wantCol {
					t.Errorf("OrderCol = %q, want %q", d.OrderCol, tc.wantCol)
				}
				if d.ByDate != tc.wantByDate {
					t.Errorf("ByDate = %v, want %v", d.ByDate, tc.wantByDate)
				}
				if got := c.Preview; got != tc.wantSQL {
					t.Errorf("Preview = %q, want %q", got, tc.wantSQL)
				}
			})
		}
	})

	t.Run("consulta de recentes: no máximo 20 linhas, ordem decrescente", func(t *testing.T) {
		c, ok := collectionByName(collections, "tbl_created")
		if !ok {
			t.Fatal("tbl_created não apareceu na listagem")
		}
		rs, err := sess.Recent(ctx, c)
		if err != nil {
			t.Fatalf("Recent: %v", err)
		}
		if len(rs.Rows) != 20 {
			t.Fatalf("len(rs.Rows) = %d, want 20 (tabela tem 25 linhas)", len(rs.Rows))
		}
		colIdx := -1
		for i, col := range rs.Columns {
			if col == "created_at" {
				colIdx = i
			}
		}
		if colIdx < 0 {
			t.Fatal("coluna created_at não veio no resultado")
		}
		for i := 1; i < len(rs.Rows); i++ {
			prev, cur := rs.Rows[i-1][colIdx], rs.Rows[i][colIdx]
			if cur > prev {
				t.Fatalf("linha %d (%s) maior que linha anterior (%s); esperava ordem decrescente", i, cur, prev)
			}
		}
	})

	t.Run("container removido ao encerrar", func(t *testing.T) {
		hint := sess.ConnectHint()
		if !containerExists(hint.Name) {
			t.Fatal("container deveria existir antes do Close()")
		}
		sess.Close()
		if containerExists(hint.Name) {
			t.Fatal("container ainda existe depois do Close()")
		}
	})
}

// TestContainerKeep caracteriza o outro lado do ciclo de vida: quando
// --keep é usado, main.go simplesmente não chama Session.Close(). Este
// teste simula os dois casos operando diretamente sobre o container Postgres
// (pgContainer, antes Container) e confirma por inspeção direta do Docker.
func TestContainerKeep(t *testing.T) {
	requireDocker(t)

	ctx := context.Background()
	cont := &pgContainer{
		Name:  uniqueName("keep"),
		Image: "postgres:16-alpine",
		Port:  freePort(),
		DB:    "verify",
		User:  "postgres",
		Pass:  "postgres",
	}
	t.Cleanup(func() { cont.Remove() })

	if err := cont.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := cont.WaitReady(ctx, 90*time.Second); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	if !containerExists(cont.Name) {
		t.Fatal("esperava container presente (simulando --keep, ou seja, sem chamar Remove())")
	}

	cont.Remove()
	if containerExists(cont.Name) {
		t.Fatal("esperava container removido após Remove() explícito")
	}
}
