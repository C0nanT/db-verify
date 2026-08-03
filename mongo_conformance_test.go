//go:build docker

package main

// Fixture de conformidade da engine MongoDB (ver conformance_test.go): como
// gerar o archive mínimo válido e o archive truncado que TestEngineConformance
// exige de toda engine registrada.
//
// Ao contrário das fixtures relacionais (postgres/mysql), que aplicam um
// schema.sql pronto, aqui os dados são semeados via driver do Mongo — não há
// um "mongoimport de schema" equivalente a um dump SQL de poucas linhas. O
// archive de conformidade em si ainda é gerado pela ferramenta de verdade
// (mongodump, dentro do container de origem), para exercitar o mesmo
// mongorestore --archive que Provision usa, não um atalho de teste.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

func init() {
	registerConformanceFixture("mongodb", ConformanceFixture{
		BuildValid:     mongoConformanceValidBackup,
		BuildTruncated: mongoConformanceTruncatedBackup,
		ValidQuery:     `{"ping": 1}`,
		InvalidQuery:   `{"estaNaoEhUmComandoDeVerdade": 1}`,
	})
}

// mongoConformanceDB é a database única usada no archive de conformidade —
// mongodump --db a isola do resto do servidor de origem (admin/local/config),
// para o archive gerado conter só as duas coleções esperadas.
const mongoConformanceDB = "lojinha"

// mongoConformanceWaitReady espera uma conexão de verdade responder a Ping —
// mesma lógica de mongoContainer.WaitReady (mongo.go), duplicada aqui porque
// o container de origem do teste não é um mongoContainer de produção.
func mongoConformanceWaitReady(t *testing.T, uri string, timeout time.Duration) *mongo.Client {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(2 * time.Second))
		if err == nil {
			if err := client.Ping(context.Background(), readpref.Primary()); err == nil {
				return client
			}
			client.Disconnect(context.Background())
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timeout esperando o MongoDB de origem ficar pronto")
	return nil
}

// mongoConformanceSeed popula com_dados (25 documentos com "criado_em"
// crescente, para exercitar o limite de 20 de Recent e a ordenação
// decrescente) e cria "vazia" explicitamente (para existir sem nenhum
// documento — mongodump só inclui uma coleção vazia no archive se ela existe
// de fato no servidor de origem).
func mongoConformanceSeed(t *testing.T, client *mongo.Client) {
	t.Helper()
	ctx := context.Background()
	db := client.Database(mongoConformanceDB)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	docs := make([]any, 25)
	for i := range docs {
		docs[i] = bson.D{
			{Key: "criado_em", Value: bson.NewDateTimeFromTime(base.Add(time.Duration(i) * 24 * time.Hour))},
			{Key: "seq", Value: i},
		}
	}
	if _, err := db.Collection("com_dados").InsertMany(ctx, docs); err != nil {
		t.Fatalf("falha ao semear com_dados: %v", err)
	}
	if err := db.CreateCollection(ctx, "vazia"); err != nil {
		t.Fatalf("falha ao criar coleção vazia: %v", err)
	}
}

// mongoConformanceSourceDump sobe um MongoDB "de origem" descartável, popula
// as coleções de conformidade, roda mongodump --archive --db dentro do
// próprio container (mesma versão de servidor e ferramenta) e devolve o
// caminho do archive copiado para fora. O container de origem é removido ao
// final do teste; o archive gerado fica num diretório temporário do teste
// (não entra no repo).
func mongoConformanceSourceDump(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	srcName := uniqueName("conf-src-mongo")
	port := freePort()
	out, err := exec.Command("docker", "run", "-d", "--name", srcName,
		"-p", fmt.Sprintf("127.0.0.1:%d:27017", port),
		"mongo:7.0").CombinedOutput()
	if err != nil {
		t.Fatalf("falha ao subir container de origem: %s", strings.TrimSpace(string(out)))
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", srcName).Run() })

	client := mongoConformanceWaitReady(t, fmt.Sprintf("mongodb://127.0.0.1:%d", port), 60*time.Second)
	defer client.Disconnect(ctx)

	mongoConformanceSeed(t, client)

	if out, err := exec.CommandContext(ctx, "docker", "exec", srcName,
		"mongodump", "--archive=/tmp/conformance.archive", "--db="+mongoConformanceDB).CombinedOutput(); err != nil {
		t.Fatalf("mongodump falhou: %s", strings.TrimSpace(string(out)))
	}

	local := t.TempDir() + "/conformance.archive"
	if out, err := exec.Command("docker", "cp", srcName+":/tmp/conformance.archive", local).CombinedOutput(); err != nil {
		t.Fatalf("docker cp falhou: %s", strings.TrimSpace(string(out)))
	}
	return local
}

// mongoConformanceValidBackup implementa ConformanceFixture.BuildValid para
// o MongoDB.
func mongoConformanceValidBackup(t *testing.T) ConformanceBackup {
	t.Helper()
	return ConformanceBackup{
		Path: mongoConformanceSourceDump(t),
		WantCollections: map[string]int64{
			"com_dados": 25,
			"vazia":     0,
		},
		DateCollection: "com_dados",
		DateColumn:     "criado_em",
	}
}

// mongoConformanceTruncatedBackup implementa
// ConformanceFixture.BuildTruncated para o MongoDB: pega um archive válido e
// corta a metade final — o formato de archive é uma sequência de blocos BSON
// com um terminador próprio (mongo-tools, common/archive/parser.go), então
// cortar no meio produz um bloco incompleto que o mongorestore rejeita como
// I/O incompleto, em vez de aceitar silenciosamente um archive parcial.
func mongoConformanceTruncatedBackup(t *testing.T) string {
	t.Helper()
	valid := mongoConformanceSourceDump(t)

	data, err := os.ReadFile(valid)
	if err != nil {
		t.Fatalf("falha ao ler archive válido: %v", err)
	}
	n := len(data) / 2
	if n < 16 {
		t.Fatalf("archive de conformidade menor do que esperado para truncar (%d bytes)", len(data))
	}

	truncPath := t.TempDir() + "/truncated.archive"
	if err := os.WriteFile(truncPath, data[:n], 0o644); err != nil {
		t.Fatalf("falha ao escrever archive truncado: %v", err)
	}
	return truncPath
}
