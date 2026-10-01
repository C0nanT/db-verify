//go:build docker

package postgres

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
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"db-verify/internal/conformance"
	"db-verify/internal/docker"
	"db-verify/internal/engine"
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
CREATE TABLE tbl_created_inserted (
    id serial PRIMARY KEY,
    created_at timestamp NOT NULL,
    inserted_at timestamp NOT NULL
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

INSERT INTO tbl_created_inserted (created_at, inserted_at) VALUES
  ('2024-01-01 10:00', '2024-01-02 10:00');

INSERT INTO tbl_pk_only (label) VALUES
  ('a'), ('b');

INSERT INTO tbl_no_option (label, other) VALUES
  ('a', 'x'), ('b', 'y');
`

// buildSourceDump sobe um Postgres "de origem" descartável, aplica o schema
// de teste e devolve o caminho de um dump em formato custom gerado por
// pg_dump dentro do próprio container (mesma versão de servidor e cliente).
// O container de origem é removido ao final do teste; o dump gerado fica
// num diretório temporário do teste (não entra no repo).
func buildSourceDump(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	srcName := conformance.UniqueName("src")
	out, err := exec.Command("docker", "run", "-d", "--name", srcName,
		"-e", "POSTGRES_PASSWORD=postgres",
		"-e", "POSTGRES_USER=postgres",
		"-e", "POSTGRES_DB=srcdb",
		"postgres:16-alpine").CombinedOutput()
	if err != nil {
		t.Fatalf("falha ao subir container de origem: %s", strings.TrimSpace(string(out)))
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", srcName).Run() })

	waitDockerPostgres(t, srcName, "postgres", "srcdb", 60*time.Second)

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

// pgBackup monta o Backup que Provision recebe a partir do cabeçalho do dump,
// sem passar pela detecção (que mora em internal/detect e não pode ser
// importada por uma engine).
func pgBackup(t *testing.T, path string) *engine.Backup {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("abrir dump: %v", err)
	}
	defer f.Close()
	head := make([]byte, 8192)
	n, err := f.Read(head)
	if err != nil {
		t.Fatalf("ler cabeçalho: %v", err)
	}
	m, _ := Engine{}.Detect(head[:n], path)
	return &engine.Backup{
		Path: path, Compression: "none", Engine: "postgres",
		Format: m.Format, Version: m.Version, OriginDB: m.OriginDB,
	}
}

func TestFullFlow_Postgres(t *testing.T) {
	conformance.RequireDocker(t)

	dumpPath := buildSourceDump(t)

	backup := pgBackup(t, dumpPath)
	if backup.Format != "custom" {
		t.Fatalf("Format = %q, want custom", backup.Format)
	}

	eng := Engine{}

	ctx := context.Background()
	sess, err := eng.Provision(ctx, backup, engine.ProvisionOpts{
		Port: new(docker.DockerHost).FreePortFrom(pgDefaultPort), Jobs: 4, DBName: "verify",
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
			"tbl_created":          25,
			"tbl_data_criacao":     3,
			"tbl_published":        2,
			"tbl_data":             2,
			"tbl_updated":          2,
			"tbl_generic_ts":       2,
			"tbl_created_inserted": 1,
			"tbl_pk_only":          2,
			"tbl_no_option":        2,
			"tbl_empty":            0,
		}
		for name, wantRows := range want {
			c, ok := conformance.CollectionByName(collections, name)
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
		c, ok := conformance.CollectionByName(collections, "tbl_empty")
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
			{"tbl_created_inserted", "created_at", true,
				`SELECT * FROM "public"."tbl_created_inserted" ORDER BY "created_at" DESC LIMIT 20;`},
			{"tbl_pk_only", "id", false,
				`SELECT * FROM "public"."tbl_pk_only" ORDER BY "id" DESC LIMIT 20;`},
			{"tbl_no_option", "", false,
				`SELECT * FROM "public"."tbl_no_option" LIMIT 20;`},
		}
		for _, tc := range cases {
			t.Run(tc.table, func(t *testing.T) {
				c, ok := conformance.CollectionByName(collections, tc.table)
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
		c, ok := conformance.CollectionByName(collections, "tbl_created")
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
		if !conformance.ContainerExists(hint.Name) {
			t.Fatal("container deveria existir antes do Close()")
		}
		sess.Close()
		if conformance.ContainerExists(hint.Name) {
			t.Fatal("container ainda existe depois do Close()")
		}
	})
}

// TestContainerKeep caracteriza o outro lado do ciclo de vida: quando
// --keep é usado, main.go simplesmente não chama Session.Close(). Este
// teste simula os dois casos operando diretamente sobre o container Postgres
// (pgContainer, antes Container) e confirma por inspeção direta do Docker.
func TestContainerKeep(t *testing.T) {
	conformance.RequireDocker(t)

	ctx := context.Background()
	cont := &pgContainer{
		Name:  conformance.UniqueName("keep"),
		Image: "postgres:16-alpine",
		Port:  new(docker.DockerHost).FreePortFrom(pgDefaultPort),
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

	if !conformance.ContainerExists(cont.Name) {
		t.Fatal("esperava container presente (simulando --keep, ou seja, sem chamar Remove())")
	}

	cont.Remove()
	if conformance.ContainerExists(cont.Name) {
		t.Fatal("esperava container removido após Remove() explícito")
	}
}
