//go:build docker

package main

// Fixture de conformidade da engine Redis (ver conformance_test.go): como
// gerar o backup mínimo válido e o backup truncado que TestEngineConformance
// exige de toda engine registrada.
//
// O "backup mínimo com duas coleções, uma vazia" que as fixtures relacionais
// seguem (postgres_conformance_test.go, mysql_conformance_test.go) não
// traduz para o Redis: uma "coleção" aqui é um grupo de chaves inferido por
// prefixo (ticket 07), e um prefixo sem nenhuma chave simplesmente não
// existe — SCAN não tem como descobri-lo. Em vez de forçar uma coleção
// vazia artificial, esta fixture usa três grupos não vazios, um deles
// deliberadamente sem separador no nome (ticket 07: "chaves sem separador
// caem num grupo próprio identificável"), o que exercita mais do contrato
// específico do Redis do que replicar o padrão relacional replicaria.
//
// DateCollection/DateColumn (exigidos por ConformanceBackup) também não têm
// equivalente direto: Redis não tem "mais recente". redisSession.Recent
// ordena a amostra por nome de chave em ordem decrescente (ver redis.go,
// sampleGroup) — um substituto determinístico documentado ali. O grupo
// "com_dados" usa chaves com sufixo numérico zero-padded (com_dados:00 …
// com_dados:24) para essa ordenação lexicográfica coincidir com a ordem de
// criação, e DateColumn é "chave" em vez do nome de uma coluna de data.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func init() {
	registerConformanceFixture("redis", ConformanceFixture{
		BuildValid:     redisConformanceValidBackup,
		BuildTruncated: redisConformanceTruncatedBackup,
		ValidQuery:     "PING",
		InvalidQuery:   "NOTACOMMAND foo bar",
	})
}

// redisConformanceSeed popula com_dados:00..24 (25 chaves, para exercitar o
// limite de 20 de Recent), poucos:0/poucos:1 (segundo grupo, não vazio) e
// "standalone" (sem separador — cai em redisNoPrefixGroup).
func redisConformanceSeed(t *testing.T, srcName string) {
	t.Helper()
	var cmds [][]string
	for i := 0; i < 25; i++ {
		cmds = append(cmds, []string{"redis-cli", "SET", fmt.Sprintf("com_dados:%02d", i), "v"})
	}
	cmds = append(cmds,
		[]string{"redis-cli", "SET", "poucos:0", "v"},
		[]string{"redis-cli", "SET", "poucos:1", "v"},
		[]string{"redis-cli", "SET", "standalone", "v"},
	)
	for _, c := range cmds {
		args := append([]string{"exec", srcName}, c...)
		if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
			t.Fatalf("falha ao popular chave de conformidade: %s", strings.TrimSpace(string(out)))
		}
	}
}

// redisConformanceSourceDump sobe um Redis "de origem" descartável, popula
// as chaves de conformidade, roda SAVE e devolve o caminho do dump.rdb
// gerado dentro do próprio container (mesma versão de servidor). O
// container de origem é removido ao final do teste; o RDB gerado fica num
// diretório temporário do teste (não entra no repo).
func redisConformanceSourceDump(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	srcName := uniqueName("conf-src-redis")
	out, err := exec.Command("docker", "run", "-d", "--name", srcName, "redis:7-alpine").CombinedOutput()
	if err != nil {
		t.Fatalf("falha ao subir container de origem: %s", strings.TrimSpace(string(out)))
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", srcName).Run() })

	deadline := time.Now().Add(30 * time.Second)
	for {
		if exec.Command("docker", "exec", srcName, "redis-cli", "PING").Run() == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout esperando o Redis de origem ficar pronto")
		}
		time.Sleep(300 * time.Millisecond)
	}

	redisConformanceSeed(t, srcName)

	if out, err := exec.CommandContext(ctx, "docker", "exec", srcName, "redis-cli", "SAVE").CombinedOutput(); err != nil {
		t.Fatalf("SAVE falhou: %s", strings.TrimSpace(string(out)))
	}

	local := t.TempDir() + "/conformance.rdb"
	if out, err := exec.Command("docker", "cp", srcName+":/data/dump.rdb", local).CombinedOutput(); err != nil {
		t.Fatalf("docker cp falhou: %s", strings.TrimSpace(string(out)))
	}
	return local
}

// redisConformanceValidBackup implementa ConformanceFixture.BuildValid para
// o Redis.
func redisConformanceValidBackup(t *testing.T) ConformanceBackup {
	t.Helper()
	return ConformanceBackup{
		Path: redisConformanceSourceDump(t),
		WantCollections: map[string]int64{
			"com_dados":        25,
			"poucos":           2,
			redisNoPrefixGroup: 1,
		},
		DateCollection: "com_dados",
		DateColumn:     "chave",
	}
}

// redisConformanceTruncatedBackup implementa
// ConformanceFixture.BuildTruncated para o Redis: pega um RDB válido e
// corta a metade final, preservando o magic "REDIS" + versão do início mas
// removendo o marcador de EOF e o checksum do fim — o servidor detecta o
// corte (offset inesperado) e sai durante o load, em vez de silenciosamente
// aceitar um arquivo incompleto (o mesmo sintoma que um RDB corrompido de
// verdade produz — ver redis.go, WaitReady).
func redisConformanceTruncatedBackup(t *testing.T) string {
	t.Helper()
	valid := redisConformanceSourceDump(t)

	data, err := os.ReadFile(valid)
	if err != nil {
		t.Fatalf("falha ao ler RDB válido: %v", err)
	}
	n := len(data) / 2
	if n < 16 {
		t.Fatalf("RDB de conformidade menor do que esperado para truncar (%d bytes)", len(data))
	}

	truncPath := t.TempDir() + "/truncated.rdb"
	if err := os.WriteFile(truncPath, data[:n], 0o644); err != nil {
		t.Fatalf("falha ao escrever RDB truncado: %v", err)
	}
	return truncPath
}
