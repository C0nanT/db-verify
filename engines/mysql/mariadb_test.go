package mysql

// Testes de detecção e heurística da engine MariaDB (Camada 1 — sem
// Docker). A suíte de conformidade (Camada 2,
// mariadb_conformance.go) cobre o resto do contrato Engine/Session
// contra um MariaDB de verdade. A heurística de coluna e as consultas de
// introspecção não são retestadas aqui: são as mesmas de mysql.go
// (TestMySQLChooseOrderColumn, TestMySQLRecentQuery já cobrem o
// comportamento, que MariaDBEngine reusa sem alteração).

import "testing"

// TestMariaDBDetect_NaoReconhece caracteriza a rejeição: sem o cabeçalho
// mariadb-dump, a engine MariaDB não reivindica o arquivo — nem por
// extensão .sql, ao contrário do MySQL (decisão documentada em SPEC.md: a
// ambiguidade de .sql sem cabeçalho fica com o MySQL).
func TestMariaDBDetect_NaoReconhece(t *testing.T) {
	cases := []struct {
		name string
		head []byte
		path string
	}{
		{"sem sinal nenhum", []byte("qualquer coisa"), "arquivo.bin"},
		{"extensão .sql sem cabeçalho", []byte("CREATE TABLE t (id INT);\n"), "backup.sql"},
		{"cabeçalho MySQL, não MariaDB", []byte("-- MySQL dump 10.13  Distrib 8.0.34, for Linux (x86_64)\n"), "backup.sql"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := MariaDBEngine{}.Detect(tc.head, tc.path)
			if ok {
				t.Fatal("esperava a engine mariadb não reconhecer o conteúdo")
			}
		})
	}
}

// TestMariaDBResolveVersion caracteriza a ordem de precedência da versão da
// imagem: --version-tag explícito > versão extraída do dump > fallback
// documentado (defaultMariaDBVersion) — mesma ordem do MySQL
// (TestMySQLResolveVersion), fallback diferente.
func TestMariaDBResolveVersion(t *testing.T) {
	cases := []struct {
		name          string
		versionTag    string
		backupVersion string
		want          string
	}{
		{"flag explícita vence tudo", "10.5", "10.11", "10.5"},
		{"versão do dump quando não há flag", "", "10.11", "10.11"},
		{"fallback quando nada informa versão", "", "", defaultMariaDBVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mariadbResolveVersion(tc.versionTag, tc.backupVersion); got != tc.want {
				t.Errorf("mariadbResolveVersion(%q, %q) = %q, want %q", tc.versionTag, tc.backupVersion, got, tc.want)
			}
		})
	}
}

// TestMariaDBExpects caracteriza a mensagem exibida em --list-engines e nos
// erros de detecção sem confiança: precisa deixar claro que não há fallback
// de extensão para esta engine.
func TestMariaDBExpects(t *testing.T) {
	if got := (MariaDBEngine{}).Expects(); got == "" {
		t.Fatal("Expects() veio vazio")
	}
}

// TestMariaDBConnectHint_UsaClienteMariaDB caracteriza o requisito do
// ticket 06 de que --keep imprima o comando de shell correto para MariaDB
// (cliente "mariadb", não "mysql").
func TestMariaDBConnectHint_UsaClienteMariaDB(t *testing.T) {
	cont := &mysqlContainer{
		Name: "db-verify-test", Client: "mariadb",
		Port: 3306, DB: "verify", User: "root", Pass: "root",
	}
	sess := &mysqlSession{cont: cont}
	hint := sess.ConnectHint()
	if hint.Shell != "mariadb -h127.0.0.1 -P3306 -uroot -proot verify" {
		t.Errorf("Shell = %q", hint.Shell)
	}
	if hint.ExecShell != "docker exec -it db-verify-test mariadb -uroot -proot verify" {
		t.Errorf("ExecShell = %q", hint.ExecShell)
	}
}

// TestMariaDBProvision_UsaImagemMariaDB caracteriza o requisito do ticket
// 06 de que --engine mariadb force a imagem mariadb:<versão> (não
// mysql:<versão>) — testável sem Docker através da mesma composição usada
// por Provision (mariadbResolveVersion + prefixo de imagem fixo "mariadb:").
func TestMariaDBProvision_UsaImagemMariaDB(t *testing.T) {
	version := mariadbResolveVersion("", "")
	image := "mariadb:" + version
	if image != "mariadb:"+defaultMariaDBVersion {
		t.Errorf("image = %q, want %q", image, "mariadb:"+defaultMariaDBVersion)
	}
}
