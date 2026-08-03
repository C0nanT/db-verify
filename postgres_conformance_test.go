//go:build docker

package main

// Fixture de conformidade da engine Postgres (ver conformance_test.go): como
// gerar o backup mínimo válido e o backup truncado que TestEngineConformance
// exige de toda engine registrada.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"os/exec"
)

func init() {
	registerConformanceFixture("postgres", ConformanceFixture{
		BuildValid:     pgConformanceValidBackup,
		BuildTruncated: pgConformanceTruncatedBackup,
		ValidQuery:     "SELECT 1",
		InvalidQuery:   "ISTO NÃO É SQL ;;;",
	})
}

// pgConformanceSchemaSQL cria o backup mínimo exigido pela suíte de
// conformidade: duas coleções, "com_dados" (25 linhas, com coluna de data
// para exercitar o limite de 20 de Recent e a ordem decrescente) e "vazia"
// (zero linhas, sem coluna de data).
const pgConformanceSchemaSQL = `
CREATE TABLE com_dados (
    id serial PRIMARY KEY,
    created_at timestamp NOT NULL
);
CREATE TABLE vazia (
    id serial PRIMARY KEY,
    label text NOT NULL
);

INSERT INTO com_dados (created_at)
  SELECT timestamp '2024-01-01' + (g * interval '1 hour')
  FROM generate_series(1, 25) AS g;
`

// pgConformanceSourceDump sobe um Postgres "de origem" descartável, aplica
// pgConformanceSchemaSQL e devolve o caminho de um dump em formato custom
// gerado por pg_dump dentro do próprio container (mesma versão de servidor e
// cliente). O container de origem é removido ao final do teste; o dump
// gerado fica num diretório temporário do teste (não entra no repo).
func pgConformanceSourceDump(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	srcName := uniqueName("conf-src")
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
	cmd.Stdin = strings.NewReader(pgConformanceSchemaSQL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("falha ao aplicar schema de conformidade: %s", strings.TrimSpace(string(out)))
	}

	if out, err := exec.Command("docker", "exec", srcName,
		"pg_dump", "-U", "postgres", "-d", "srcdb", "-Fc", "-f", "/tmp/conformance.dump").CombinedOutput(); err != nil {
		t.Fatalf("pg_dump falhou: %s", strings.TrimSpace(string(out)))
	}

	local := t.TempDir() + "/conformance.dump"
	if out, err := exec.Command("docker", "cp", srcName+":/tmp/conformance.dump", local).CombinedOutput(); err != nil {
		t.Fatalf("docker cp falhou: %s", strings.TrimSpace(string(out)))
	}
	return local
}

// pgConformanceValidBackup implementa ConformanceFixture.BuildValid para o
// Postgres.
func pgConformanceValidBackup(t *testing.T) ConformanceBackup {
	t.Helper()
	return ConformanceBackup{
		Path: pgConformanceSourceDump(t),
		WantCollections: map[string]int64{
			"com_dados": 25,
			"vazia":     0,
		},
		DateCollection: "com_dados",
		DateColumn:     "created_at",
	}
}

// pgConformanceTruncatedBackup implementa ConformanceFixture.BuildTruncated
// para o Postgres: pega um dump custom válido e corta a maior parte do
// conteúdo, preservando só um pedaço do início — o suficiente para a
// detecção reconhecer o formato (magic "PGDMP"), mas insuficiente para
// pg_restore concluir sem erro.
func pgConformanceTruncatedBackup(t *testing.T) string {
	t.Helper()
	valid := pgConformanceSourceDump(t)

	data, err := os.ReadFile(valid)
	if err != nil {
		t.Fatalf("falha ao ler dump válido: %v", err)
	}

	n := len(data) / 4
	if n < 64 {
		n = 64
	}
	if n > len(data) {
		n = len(data)
	}

	truncPath := t.TempDir() + "/truncated.dump"
	if err := os.WriteFile(truncPath, data[:n], 0o644); err != nil {
		t.Fatalf("falha ao escrever dump truncado: %v", err)
	}
	return truncPath
}
