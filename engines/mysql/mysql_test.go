package mysql

// Testes de detecção e heurística da engine MySQL (Camada 1 — sem Docker).
// A suíte de conformidade (Camada 2, mysql_conformance.go) cobre o
// resto do contrato Engine/Session contra um MySQL de verdade.

import (
	"testing"

	"db-verify/internal/engine"
)

// TestMySQLDetect_ExtensaoSemCabecalho caracteriza o sinal de confiança
// média por extensão .sql (ticket 05): um arquivo com essa extensão mas sem
// o cabeçalho "-- MySQL dump" ainda é reconhecido pela engine MySQL quando
// consultada diretamente, com confiança de extensão (não magia, não
// palpite).
func TestMySQLDetect_ExtensaoSemCabecalho(t *testing.T) {
	m, ok := Engine{}.Detect([]byte("CREATE TABLE t (id INT);\n"), "backup.sql")
	if !ok {
		t.Fatal("esperava a engine mysql reconhecer .sql mesmo sem cabeçalho")
	}
	if m.Confidence != engine.ConfidenceExtension {
		t.Errorf("Confidence = %d, want %d (engine.ConfidenceExtension)", m.Confidence, engine.ConfidenceExtension)
	}
	if m.Format != "sql" {
		t.Errorf("Format = %q, want sql", m.Format)
	}
}

// TestMySQLDetect_NaoReconhece caracteriza a rejeição: sem o cabeçalho
// mysqldump e sem extensão .sql, a engine MySQL não reivindica o arquivo.
func TestMySQLDetect_NaoReconhece(t *testing.T) {
	_, ok := Engine{}.Detect([]byte("qualquer coisa"), "arquivo.bin")
	if ok {
		t.Fatal("esperava a engine mysql não reconhecer conteúdo sem sinal nenhum")
	}
}

// TestMySQLResolveVersion caracteriza a ordem de precedência da versão da
// imagem: --version-tag explícito > versão extraída do dump > fallback
// documentado (defaultMySQLVersion).
func TestMySQLResolveVersion(t *testing.T) {
	cases := []struct {
		name          string
		versionTag    string
		backupVersion string
		want          string
	}{
		{"flag explícita vence tudo", "5.7", "8.0", "5.7"},
		{"versão do dump quando não há flag", "", "8.0", "8.0"},
		{"fallback quando nada informa versão", "", "", defaultMySQLVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mysqlResolveVersion(tc.versionTag, tc.backupVersion); got != tc.want {
				t.Errorf("mysqlResolveVersion(%q, %q) = %q, want %q", tc.versionTag, tc.backupVersion, got, tc.want)
			}
		})
	}
}

// TestMySQLChooseOrderColumn caracteriza a heurística compartilhada aplicada
// ao MySQL: mesmos nomes (incluindo português) que o Postgres, mais o
// fallback para PK simples e o caso sem nenhuma opção — espelhando
// TestFullFlow_Postgres/heurística de coluna de ordenação.
func TestMySQLChooseOrderColumn(t *testing.T) {
	cases := []struct {
		name       string
		cols       []mysqlColumn
		pk         string
		wantCol    string
		wantByDate bool
	}{
		{"created_at", []mysqlColumn{{Name: "id", DataType: "int"}, {Name: "created_at", DataType: "datetime"}}, "id", "created_at", true},
		{"nome em português: data_criacao", []mysqlColumn{{Name: "data_criacao", DataType: "date"}}, "", "data_criacao", true},
		{"nome em português: atualizado_em", []mysqlColumn{{Name: "atualizado_em", DataType: "datetime"}}, "", "atualizado_em", true},
		{"timestamp genérico sem nome conhecido", []mysqlColumn{{Name: "some_moment", DataType: "timestamp"}}, "id", "some_moment", true},
		{"só PK, sem coluna de data", []mysqlColumn{{Name: "id", DataType: "int"}, {Name: "label", DataType: "text"}}, "id", "id", false},
		{"nem PK nem data", []mysqlColumn{{Name: "label", DataType: "text"}}, "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			col, byDate := chooseOrderColumn(tc.cols, tc.pk)
			if col != tc.wantCol {
				t.Errorf("col = %q, want %q", col, tc.wantCol)
			}
			if byDate != tc.wantByDate {
				t.Errorf("byDate = %v, want %v", byDate, tc.wantByDate)
			}
		})
	}
}

// TestMySQLRecentQuery caracteriza o SQL nativo gerado para os 20 mais
// recentes, com e sem coluna de ordenação — precisa ser copiável para um
// cliente mysql de verdade (ticket 05).
func TestMySQLRecentQuery(t *testing.T) {
	got := mysqlRecentQuery("verify", "com_dados", mysqlDescriptor{OrderCol: "created_at", ByDate: true})
	want := "SELECT * FROM `verify`.`com_dados` ORDER BY `created_at` DESC LIMIT 20;"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	got = mysqlRecentQuery("verify", "vazia", mysqlDescriptor{})
	want = "SELECT * FROM `verify`.`vazia` LIMIT 20;"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
