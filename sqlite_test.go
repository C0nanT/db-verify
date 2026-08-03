package main

// Testes da engine SQLite (ticket 08). Ao contrário das demais engines
// relacionais, o fluxo inteiro (Provision → Session) não depende de Docker
// — então, diferente de mysql_test.go/mariadb_test.go (que só cobrem
// detecção e heurística sem Docker, deixando o resto para a suíte de
// conformidade atrás da build tag "docker"), este arquivo caracteriza o
// contrato Engine/Session inteiro sem tag nenhuma: é exatamente o cenário
// que o ticket pede — "verificar um SQLite funciona numa máquina sem
// Docker".

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// buildSQLiteFixture cria um banco SQLite de verdade num diretório
// temporário do teste, com duas tabelas — "com_dados" (25 linhas, coluna
// created_at, para exercitar o limite de 20 de Recent e a ordem
// decrescente) e "vazia" (zero linhas, sem coluna de data) — e devolve o
// caminho do arquivo.
func buildSQLiteFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("abrindo fixture: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE com_dados (id INTEGER PRIMARY KEY, created_at DATETIME NOT NULL)`,
		`CREATE TABLE vazia (id INTEGER PRIMARY KEY, label TEXT NOT NULL)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("criando schema de fixture: %v", err)
		}
	}
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 25; i++ {
		ts := base.Add(time.Duration(i) * time.Hour).Format("2006-01-02 15:04:05")
		if _, err := db.Exec(`INSERT INTO com_dados (created_at) VALUES (?)`, ts); err != nil {
			t.Fatalf("populando fixture: %v", err)
		}
	}
	return path
}

// TestSQLiteDetect_Magic caracteriza o reconhecimento pelo cabeçalho fixo
// de 16 bytes.
func TestSQLiteDetect_Magic(t *testing.T) {
	m, ok := sqliteEngine{}.Detect([]byte("SQLite format 3\x00resto do cabeçalho"), "backup.bin")
	if !ok {
		t.Fatal("esperava a engine sqlite reconhecer o magic")
	}
	if m.Confidence != ConfidenceMagic {
		t.Errorf("Confidence = %d, want %d (ConfidenceMagic)", m.Confidence, ConfidenceMagic)
	}
	if m.Format != "sqlite3" {
		t.Errorf("Format = %q, want sqlite3", m.Format)
	}
}

// TestSQLiteDetect_Extensao caracteriza o sinal de confiança média para as
// extensões .sqlite e .db, mesmo sem o magic no início do arquivo.
func TestSQLiteDetect_Extensao(t *testing.T) {
	cases := []string{"app.sqlite", "app.db", "APP.DB"}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			m, ok := sqliteEngine{}.Detect([]byte("lixo qualquer, sem magic"), path)
			if !ok {
				t.Fatalf("esperava a engine sqlite reconhecer %q por extensão", path)
			}
			if m.Confidence != ConfidenceExtension {
				t.Errorf("Confidence = %d, want %d (ConfidenceExtension)", m.Confidence, ConfidenceExtension)
			}
		})
	}
}

// TestSQLiteDetect_NaoReconhece caracteriza a rejeição: sem magic e sem
// extensão .sqlite/.db, a engine não reivindica o arquivo.
func TestSQLiteDetect_NaoReconhece(t *testing.T) {
	_, ok := sqliteEngine{}.Detect([]byte("qualquer coisa"), "arquivo.bin")
	if ok {
		t.Fatal("esperava a engine sqlite não reconhecer conteúdo sem sinal nenhum")
	}
}

// TestSQLiteDetect_Header e TestSQLiteDetect_Gzip caracterizam a detecção
// via InspectDump usando as fixtures reais em testdata/headers, incluindo
// comprimida — mesmo padrão das demais engines (ver mysql_test.go).
func TestSQLiteDetect_Header(t *testing.T) {
	info, err := InspectDump("testdata/headers/sqlite.db")
	if err != nil {
		t.Fatalf("InspectDump: %v", err)
	}
	if info.Engine != "sqlite" {
		t.Fatalf("Engine = %q, want sqlite", info.Engine)
	}
	if info.Guessed {
		t.Errorf("esperava Guessed=false para cabeçalho reconhecido por magic bytes")
	}
}

func TestSQLiteDetect_Gzip(t *testing.T) {
	info, err := InspectDump("testdata/headers/sqlite.db.gz")
	if err != nil {
		t.Fatalf("InspectDump: %v", err)
	}
	if info.Compression != "gzip" {
		t.Errorf("Compression = %q, want gzip", info.Compression)
	}
	if info.Engine != "sqlite" {
		t.Errorf("Engine = %q, want sqlite", info.Engine)
	}
}

// TestSQLiteProvision_SemDockerFuncionaEEhInstantaneo é o teste central do
// ticket: Provision funciona de ponta a ponta sem checar Docker nenhum, e
// termina rápido — bem abaixo de qualquer timeout de "servidor ficar
// pronto" que as demais engines precisam.
func TestSQLiteProvision_SemDockerFuncionaEEhInstantaneo(t *testing.T) {
	path := buildSQLiteFixture(t)
	backup, err := InspectDumpAs(path, "sqlite")
	if err != nil {
		t.Fatalf("InspectDumpAs: %v", err)
	}

	start := time.Now()
	sess, err := sqliteEngine{}.Provision(context.Background(), backup, ProvisionOpts{})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	t.Cleanup(func() { sess.Close() })

	if elapsed > 2*time.Second {
		t.Errorf("Provision levou %s — esperava algo instantâneo, sem container", elapsed)
	}

	res := sess.Restore()
	if res == nil || len(res.Errors) != 0 {
		t.Fatalf("esperava restore sem erros, tive %+v", res)
	}
}

// TestSQLiteProvision_ArquivoOriginalNuncaEhModificado caracteriza a
// garantia central de segurança da engine: a verificação roda sobre uma
// cópia, o arquivo original do usuário não é tocado.
func TestSQLiteProvision_ArquivoOriginalNuncaEhModificado(t *testing.T) {
	path := buildSQLiteFixture(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lendo fixture antes: %v", err)
	}
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat antes: %v", err)
	}

	backup, err := InspectDumpAs(path, "sqlite")
	if err != nil {
		t.Fatalf("InspectDumpAs: %v", err)
	}
	sess, err := sqliteEngine{}.Provision(context.Background(), backup, ProvisionOpts{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	ctx := context.Background()
	if _, err := sess.Collections(ctx, true); err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if _, err := sess.Query(ctx, "INSERT INTO com_dados (created_at) VALUES ('2099-01-01 00:00:00')"); err != nil {
		t.Fatalf("Query (insert de teste na cópia): %v", err)
	}
	sess.Close()

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lendo fixture depois: %v", err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat depois: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("o conteúdo do arquivo original mudou depois de Provision/Query/Close")
	}
	if beforeInfo.ModTime() != afterInfo.ModTime() {
		t.Fatal("o mtime do arquivo original mudou depois de Provision/Query/Close")
	}
}

// TestSQLiteProvision_ArquivoCorrompidoProduzErroDeRestore caracteriza que
// um arquivo corrompido produz erros de restore reportados — não um
// pânico, não um Provision que falha genérico demais para main.go
// distinguir "arquivo ruim" de "faltou alguma dependência".
func TestSQLiteProvision_ArquivoCorrompidoProduzErroDeRestore(t *testing.T) {
	valid := buildSQLiteFixture(t)
	data, err := os.ReadFile(valid)
	if err != nil {
		t.Fatalf("lendo fixture válida: %v", err)
	}
	if len(data) < 2048 {
		t.Fatalf("fixture menor do que esperado para truncar (%d bytes)", len(data))
	}
	truncPath := filepath.Join(t.TempDir(), "corrupted.sqlite")
	// preserva o cabeçalho (magic reconhecível) mas corta o arquivo no meio
	// das páginas de dados — arquivo sintaticamente "é SQLite", mas com
	// b-tree incompleta.
	if err := os.WriteFile(truncPath, data[:len(data)*2/3], 0o644); err != nil {
		t.Fatalf("escrevendo fixture truncada: %v", err)
	}

	backup, err := InspectDumpAs(truncPath, "sqlite")
	if err != nil {
		t.Fatalf("InspectDumpAs: %v", err)
	}
	sess, err := sqliteEngine{}.Provision(context.Background(), backup, ProvisionOpts{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	t.Cleanup(func() { sess.Close() })

	res := sess.Restore()
	if res == nil {
		t.Fatal("esperava RestoreResult não nulo")
	}
	if len(res.Errors) == 0 {
		t.Fatal("esperava erros de restore reportados para um arquivo truncado, veio zero")
	}
	if res.LogPath == "" {
		t.Fatal("esperava log do restore em arquivo (RestoreResult.LogPath vazio)")
	}
	if _, err := os.Stat(res.LogPath); err != nil {
		t.Fatalf("log do restore não encontrado em %q: %v", res.LogPath, err)
	}
}

// TestSQLiteProvision_ArquivoNaoEhSQLite caracteriza o caso em que
// --engine sqlite é forçado contra um arquivo que não é um banco SQLite de
// jeito nenhum: erro de restore, não pânico nem erro de Provision.
func TestSQLiteProvision_ArquivoNaoEhSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nao-e-sqlite.db")
	if err := os.WriteFile(path, []byte(strings.Repeat("isto não é um banco sqlite ", 20)), 0o644); err != nil {
		t.Fatalf("escrevendo arquivo de teste: %v", err)
	}
	backup, err := InspectDumpAs(path, "sqlite")
	if err != nil {
		t.Fatalf("InspectDumpAs: %v", err)
	}
	sess, err := sqliteEngine{}.Provision(context.Background(), backup, ProvisionOpts{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	t.Cleanup(func() { sess.Close() })

	res := sess.Restore()
	if res == nil || len(res.Errors) == 0 {
		t.Fatalf("esperava erro de restore para arquivo que não é SQLite, tive %+v", res)
	}
}

// TestSQLiteSession_HealthSemCamposInaplicaveis caracteriza que Health
// devolve só contadores que existem de verdade em SQLite.
func TestSQLiteSession_HealthSemCamposInaplicaveis(t *testing.T) {
	path := buildSQLiteFixture(t)
	backup, err := InspectDumpAs(path, "sqlite")
	if err != nil {
		t.Fatalf("InspectDumpAs: %v", err)
	}
	sess, err := sqliteEngine{}.Provision(context.Background(), backup, ProvisionOpts{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	t.Cleanup(func() { sess.Close() })

	health, err := sess.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if health.Name == "" {
		t.Error("Health.Name veio vazio")
	}
	if health.Size == "" {
		t.Error("Health.Size veio vazio")
	}
	wantLabels := map[string]string{
		"tabelas":  "2",
		"gatilhos": "0",
		"fks":      "0",
	}
	seen := map[string]bool{}
	for _, f := range health.Fields {
		seen[f.Label] = true
		if want, ok := wantLabels[f.Label]; ok && f.Value != want {
			t.Errorf("campo %q = %q, want %q", f.Label, f.Value, want)
		}
		if strings.EqualFold(f.Label, "procedures") {
			t.Errorf("Health não deveria publicar %q — SQLite não tem esse conceito", f.Label)
		}
	}
	for label := range wantLabels {
		if !seen[label] {
			t.Errorf("campo %q não apareceu em Health.Fields (%+v)", label, health.Fields)
		}
	}
}

// TestSQLiteSession_Collections caracteriza a listagem de tabelas com
// contagem exata, incluindo a tabela vazia (não omitida).
func TestSQLiteSession_Collections(t *testing.T) {
	path := buildSQLiteFixture(t)
	backup, err := InspectDumpAs(path, "sqlite")
	if err != nil {
		t.Fatalf("InspectDumpAs: %v", err)
	}
	sess, err := sqliteEngine{}.Provision(context.Background(), backup, ProvisionOpts{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	t.Cleanup(func() { sess.Close() })

	collections, err := sess.Collections(context.Background(), true)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if len(collections) != 2 {
		t.Fatalf("len(collections) = %d, want 2 (%+v)", len(collections), collections)
	}
	c, ok := sqliteCollectionByName(collections, "com_dados")
	if !ok {
		t.Fatal("com_dados não apareceu na listagem")
	}
	if c.Count != 25 {
		t.Errorf("com_dados: Count = %d, want 25", c.Count)
	}
	if c.Size == "" {
		t.Error("com_dados: Size veio vazio")
	}
	d, ok := c.Descriptor.(sqliteDescriptor)
	if !ok {
		t.Fatalf("Descriptor não é sqliteDescriptor: %#v", c.Descriptor)
	}
	if d.OrderCol != "created_at" || !d.ByDate {
		t.Errorf("com_dados: descriptor = %+v, want OrderCol=created_at ByDate=true", d)
	}
	wantSQL := `SELECT * FROM "com_dados" ORDER BY "created_at" DESC LIMIT 20;`
	if c.Preview != wantSQL {
		t.Errorf("Preview = %q, want %q", c.Preview, wantSQL)
	}

	vazia, ok := sqliteCollectionByName(collections, "vazia")
	if !ok {
		t.Fatal("vazia não apareceu na listagem — tabela vazia não deveria ser omitida")
	}
	if vazia.Count != 0 {
		t.Errorf("vazia: Count = %d, want 0", vazia.Count)
	}
}

// TestSQLiteSession_Recent caracteriza o limite de 20 linhas e a ordem
// decrescente por created_at — mesma heurística compartilhada das demais
// engines relacionais.
func TestSQLiteSession_Recent(t *testing.T) {
	path := buildSQLiteFixture(t)
	backup, err := InspectDumpAs(path, "sqlite")
	if err != nil {
		t.Fatalf("InspectDumpAs: %v", err)
	}
	sess, err := sqliteEngine{}.Provision(context.Background(), backup, ProvisionOpts{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	t.Cleanup(func() { sess.Close() })

	ctx := context.Background()
	collections, err := sess.Collections(ctx, true)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	c, ok := sqliteCollectionByName(collections, "com_dados")
	if !ok {
		t.Fatal("com_dados não apareceu na listagem")
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
	if rs.Query == "" {
		t.Error("ResultSet.Query veio vazio — deveria trazer o SQL copiável para o sqlite3")
	}
}

// TestSQLiteSession_Query caracteriza consulta nativa válida e inválida,
// sem pânico — mesmo contrato exigido pela suíte de conformidade.
func TestSQLiteSession_Query(t *testing.T) {
	path := buildSQLiteFixture(t)
	backup, err := InspectDumpAs(path, "sqlite")
	if err != nil {
		t.Fatalf("InspectDumpAs: %v", err)
	}
	sess, err := sqliteEngine{}.Provision(context.Background(), backup, ProvisionOpts{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	t.Cleanup(func() { sess.Close() })

	ctx := context.Background()
	if _, err := sess.Query(ctx, "SELECT 1"); err != nil {
		t.Fatalf("Query(SELECT 1): %v", err)
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Query entrou em pânico com comando inválido: %v", r)
			}
		}()
		if _, err := sess.Query(ctx, "ISTO NÃO É SQL ;;;"); err == nil {
			t.Fatal("esperava erro para comando inválido, veio nil")
		}
	}()
}

// TestSQLiteSession_ConnectHintSemContainer caracteriza o que diferencia
// esta engine de todas as outras: ConnectHint.Name vazio (não há container
// para nomear/remover) e um Shell copiável para o cliente sqlite3.
func TestSQLiteSession_ConnectHintSemContainer(t *testing.T) {
	path := buildSQLiteFixture(t)
	backup, err := InspectDumpAs(path, "sqlite")
	if err != nil {
		t.Fatalf("InspectDumpAs: %v", err)
	}
	sess, err := sqliteEngine{}.Provision(context.Background(), backup, ProvisionOpts{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	t.Cleanup(func() { sess.Close() })

	hint := sess.ConnectHint()
	if hint.Name != "" {
		t.Errorf("ConnectHint.Name = %q, want vazio (engine sem container)", hint.Name)
	}
	if hint.DSN == "" {
		t.Error("ConnectHint.DSN veio vazio")
	}
	if !strings.HasPrefix(hint.Shell, "sqlite3 ") {
		t.Errorf("ConnectHint.Shell = %q, want prefixo \"sqlite3 \"", hint.Shell)
	}
}

// TestSQLiteSession_CloseRemoveTemporario e
// TestSQLiteSession_KeepPreservaTemporario caracterizam o ciclo de vida do
// arquivo temporário: Close remove, e não chamar Close (o que main.go faz
// quando --keep está ligado) preserva o arquivo — o próprio mecanismo de
// --keep das demais engines, adaptado de "não remover o container" para
// "não remover o temporário".
func TestSQLiteSession_CloseRemoveTemporario(t *testing.T) {
	path := buildSQLiteFixture(t)
	backup, err := InspectDumpAs(path, "sqlite")
	if err != nil {
		t.Fatalf("InspectDumpAs: %v", err)
	}
	sess, err := sqliteEngine{}.Provision(context.Background(), backup, ProvisionOpts{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	tmpPath := sess.ConnectHint().DSN
	if _, err := os.Stat(tmpPath); err != nil {
		t.Fatalf("temporário deveria existir antes do Close(): %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("temporário ainda existe depois do Close() (err=%v)", err)
	}
}

func TestSQLiteSession_KeepPreservaTemporario(t *testing.T) {
	path := buildSQLiteFixture(t)
	backup, err := InspectDumpAs(path, "sqlite")
	if err != nil {
		t.Fatalf("InspectDumpAs: %v", err)
	}
	sess, err := sqliteEngine{}.Provision(context.Background(), backup, ProvisionOpts{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	tmpPath := sess.ConnectHint().DSN
	t.Cleanup(func() { os.Remove(tmpPath) })

	// simula --keep: main.go simplesmente não chama Close() nesse caso.
	if _, err := os.Stat(tmpPath); err != nil {
		t.Fatalf("temporário deveria existir (simulando --keep): %v", err)
	}
}

// sqliteCollectionByName é uma cópia local do helper de busca linear que
// docker_test.go define (collectionByName) — este arquivo roda sem a build
// tag "docker", então não pode depender de símbolos que só existem atrás
// dela.
func sqliteCollectionByName(collections []Collection, name string) (Collection, bool) {
	for _, c := range collections {
		if c.Name == name {
			return c, true
		}
	}
	return Collection{}, false
}
