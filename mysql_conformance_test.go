//go:build docker

package main

// Fixture de conformidade da engine MySQL (ver conformance_test.go): como
// gerar o backup mínimo válido e o backup truncado que TestEngineConformance
// exige de toda engine registrada.

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"os/exec"
)

func init() {
	registerConformanceFixture("mysql", ConformanceFixture{
		BuildValid:     mysqlConformanceValidBackup,
		BuildTruncated: mysqlConformanceTruncatedBackup,
		ValidQuery:     "SELECT 1",
		InvalidQuery:   "ISTO NÃO É SQL ;;;",
	})
}

// mysqlConformanceSchemaSQL cria o backup mínimo exigido pela suíte de
// conformidade: duas coleções, "com_dados" (25 linhas, com coluna de data
// para exercitar o limite de 20 de Recent e a ordem decrescente) e "vazia"
// (zero linhas, sem coluna de data).
var mysqlConformanceSchemaSQL = `
CREATE TABLE com_dados (
    id INT AUTO_INCREMENT PRIMARY KEY,
    created_at DATETIME NOT NULL
);
CREATE TABLE vazia (
    id INT AUTO_INCREMENT PRIMARY KEY,
    label TEXT NOT NULL
);

INSERT INTO com_dados (created_at) VALUES
` + mysqlConformanceSeedRows() + `;
`

// mysqlConformanceSeedRows gera 25 linhas de created_at, uma por hora a
// partir de 2024-01-01 — mesmo formato do fixture de Postgres.
func mysqlConformanceSeedRows() string {
	var b strings.Builder
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 25; i++ {
		if i > 1 {
			b.WriteString(",\n")
		}
		b.WriteString("('" + base.Add(time.Duration(i)*time.Hour).Format("2006-01-02 15:04:05") + "')")
	}
	return b.String()
}

// waitMySQLReady espera até duas consultas autenticadas seguidas funcionarem
// (com um respiro entre elas). Uma só checagem de sucesso não basta: a
// imagem oficial reinicia o servidor entre a fase de inicialização e a fase
// final (ver mysql.go, mysqlContainer.WaitReady), e testar bem na fronteira
// desse reinício pode achar o socket momentaneamente fechado logo depois de
// um "SELECT 1" ter funcionado — daí o debounce.
func waitMySQLReady(t *testing.T, name, user, pass string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	check := func() bool {
		return exec.Command("docker", "exec", name, "mysql", "-u"+user, "-p"+pass, "-e", "SELECT 1").Run() == nil
	}
	for time.Now().Before(deadline) {
		if check() {
			time.Sleep(300 * time.Millisecond)
			if check() {
				return
			}
			continue
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timeout esperando o MySQL (%s) ficar pronto", name)
}

// mysqlConformanceSourceDump sobe um MySQL "de origem" descartável, aplica
// mysqlConformanceSchemaSQL e devolve o caminho de um dump gerado por
// mysqldump dentro do próprio container (mesma versão de servidor e
// cliente). O container de origem é removido ao final do teste; o dump
// gerado fica num diretório temporário do teste (não entra no repo).
func mysqlConformanceSourceDump(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	srcName := uniqueName("conf-src-mysql")
	out, err := exec.Command("docker", "run", "-d", "--name", srcName,
		"-e", "MYSQL_ROOT_PASSWORD=root",
		"-e", "MYSQL_DATABASE=srcdb",
		"mysql:8.0").CombinedOutput()
	if err != nil {
		t.Fatalf("falha ao subir container de origem: %s", strings.TrimSpace(string(out)))
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", srcName).Run() })

	waitMySQLReady(t, srcName, "root", "root", 90*time.Second)

	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", srcName,
		"mysql", "-uroot", "-proot", "srcdb")
	cmd.Stdin = strings.NewReader(mysqlConformanceSchemaSQL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("falha ao aplicar schema de conformidade: %s", strings.TrimSpace(string(out)))
	}

	if out, err := exec.Command("docker", "exec", srcName, "sh", "-c",
		"mysqldump -uroot -proot srcdb > /tmp/conformance.sql").CombinedOutput(); err != nil {
		t.Fatalf("mysqldump falhou: %s", strings.TrimSpace(string(out)))
	}

	local := t.TempDir() + "/conformance.sql"
	if out, err := exec.Command("docker", "cp", srcName+":/tmp/conformance.sql", local).CombinedOutput(); err != nil {
		t.Fatalf("docker cp falhou: %s", strings.TrimSpace(string(out)))
	}
	return local
}

// mysqlConformanceValidBackup implementa ConformanceFixture.BuildValid para
// o MySQL.
func mysqlConformanceValidBackup(t *testing.T) ConformanceBackup {
	t.Helper()
	return ConformanceBackup{
		Path: mysqlConformanceSourceDump(t),
		WantCollections: map[string]int64{
			"com_dados": 25,
			"vazia":     0,
		},
		DateCollection: "com_dados",
		DateColumn:     "created_at",
	}
}

// mysqlConformanceTruncatedBackup implementa ConformanceFixture.BuildTruncated
// para o MySQL: pega um dump válido e corta bem no meio da lista de valores
// do INSERT — diferente do dump binário do Postgres (onde qualquer corte
// corrompe o cabeçalho), o mysqldump é texto, e cortar num ponto qualquer
// (ex.: 1/4 do arquivo) tende a cair dentro de um comentário "--" ou entre
// statements completos, o que o cliente mysql aceita sem erro (só executa
// menos coisa, silenciosamente). Cortar dentro da tupla de valores garante
// um statement sintaticamente incompleto — erro de verdade, não silêncio.
func mysqlConformanceTruncatedBackup(t *testing.T) string {
	t.Helper()
	valid := mysqlConformanceSourceDump(t)

	data, err := os.ReadFile(valid)
	if err != nil {
		t.Fatalf("falha ao ler dump válido: %v", err)
	}

	idx := bytes.Index(data, []byte("INSERT INTO"))
	if idx < 0 {
		t.Fatal("dump de conformidade não contém INSERT INTO — fixture mudou?")
	}
	cut := idx + 200 // bem dentro da lista de tuplas de com_dados (25 linhas)
	if cut >= len(data) {
		t.Fatalf("dump de conformidade menor do que esperado para truncar dentro do INSERT (%d bytes)", len(data))
	}

	truncPath := t.TempDir() + "/truncated.sql"
	if err := os.WriteFile(truncPath, data[:cut], 0o644); err != nil {
		t.Fatalf("falha ao escrever dump truncado: %v", err)
	}
	return truncPath
}
