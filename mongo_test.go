package main

// Testes de unidade da engine MongoDB (Camada 1 — sem Docker): detecção do
// magic number e extração de versão do cabeçalho do archive, resolução de
// versão de imagem, achatamento de documentos aninhados, heurística de
// campo de ordenação, e o texto da consulta nativa copiável.

import (
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// buildMongoArchiveHeader monta um cabeçalho de archive sintético: magic
// number + o documento BSON (Header, ver mongo-tools/common/archive/archive.go)
// que o mongodump escreve logo em seguida, contendo pelo menos
// "server_version" — suficiente para exercitar Detect sem precisar de um
// archive de verdade gerado por mongodump.
func buildMongoArchiveHeader(t *testing.T, serverVersion string) []byte {
	t.Helper()
	doc, err := bson.Marshal(bson.D{
		{Key: "concurrent_collections", Value: int32(4)},
		{Key: "version", Value: "0.1"},
		{Key: "server_version", Value: serverVersion},
		{Key: "tool_version", Value: "100.9.4"},
	})
	if err != nil {
		t.Fatalf("bson.Marshal: %v", err)
	}
	return append(append([]byte{}, mongoArchiveMagic...), doc...)
}

func TestMongoEngine_Detect_MagicComVersao(t *testing.T) {
	head := buildMongoArchiveHeader(t, "7.0.14")
	m, ok := mongoEngine{}.Detect(head, "backup.archive")
	if !ok {
		t.Fatal("esperava reconhecer o archive pelo magic number")
	}
	if m.Format != "archive" {
		t.Errorf("Format = %q, want archive", m.Format)
	}
	if m.Confidence != ConfidenceMagic {
		t.Errorf("Confidence = %d, want %d", m.Confidence, ConfidenceMagic)
	}
	if m.Version != "7.0" {
		t.Errorf("Version = %q, want 7.0 (major.minor de 7.0.14)", m.Version)
	}
}

func TestMongoEngine_Detect_MagicSemVersaoReconhecivel(t *testing.T) {
	// magic number seguido de lixo não-BSON: ainda reconhece o formato,
	// só não extrai versão.
	head := append(append([]byte{}, mongoArchiveMagic...), []byte("lixo qualquer")...)
	m, ok := mongoEngine{}.Detect(head, "backup.archive")
	if !ok {
		t.Fatal("esperava reconhecer pelo magic number mesmo sem cabeçalho BSON válido")
	}
	if m.Version != "" {
		t.Errorf("Version = %q, want vazio", m.Version)
	}
}

func TestMongoEngine_Detect_ExtensaoSemMagic(t *testing.T) {
	m, ok := mongoEngine{}.Detect([]byte("qualquer coisa"), "backup.archive")
	if !ok {
		t.Fatal("esperava reconhecer pela extensão .archive")
	}
	if m.Confidence != ConfidenceExtension {
		t.Errorf("Confidence = %d, want %d", m.Confidence, ConfidenceExtension)
	}
}

func TestMongoEngine_Detect_NadaReconhecido(t *testing.T) {
	_, ok := mongoEngine{}.Detect([]byte("qualquer coisa"), "backup.bin")
	if ok {
		t.Fatal("não deveria reconhecer arquivo sem magic number nem extensão .archive")
	}
}

func TestResolveMongoVersion(t *testing.T) {
	cases := []struct {
		versionTag, backupVersion, want string
	}{
		{"6.0", "7.0", "6.0"},         // --version-tag explícito vence
		{"", "7.0", "7.0"},            // senão, a versão extraída do archive
		{"", "", defaultMongoVersion}, // senão, o fallback documentado
	}
	for _, tc := range cases {
		if got := resolveMongoVersion(tc.versionTag, tc.backupVersion); got != tc.want {
			t.Errorf("resolveMongoVersion(%q, %q) = %q, want %q", tc.versionTag, tc.backupVersion, got, tc.want)
		}
	}
}

func TestMongoFlattenInto_CaminhoPontilhadoAteProfundidade(t *testing.T) {
	// 4 níveis de aninhamento — um a mais que mongoFlattenDepth (3) — para
	// que o último (interno) estoure a profundidade e vire JSON compacto em
	// vez de continuar achatando até "bairro".
	doc := bson.M{
		"nome": "Ana",
		"endereco": bson.M{
			"cidade": "Recife",
			"geo": bson.M{
				"lat": 1.5,
				"detalhe": bson.M{
					"interno": bson.M{
						"bairro": "Boa Viagem",
					},
				},
			},
		},
	}
	out := map[string]string{}
	mongoFlattenInto(doc, "", mongoFlattenDepth, out)

	if out["nome"] != "Ana" {
		t.Errorf(`out["nome"] = %q, want "Ana"`, out["nome"])
	}
	if out["endereco.cidade"] != "Recife" {
		t.Errorf(`out["endereco.cidade"] = %q, want "Recife"`, out["endereco.cidade"])
	}
	if out["endereco.geo.lat"] != "1.5" {
		t.Errorf(`out["endereco.geo.lat"] = %q, want "1.5"`, out["endereco.geo.lat"])
	}
	// além da profundidade fixa, o sub-documento vira JSON compacto no
	// próprio caminho, em vez de continuar achatando.
	got, ok := out["endereco.geo.detalhe.interno"]
	if !ok {
		t.Fatalf(`esperava a chave "endereco.geo.detalhe.interno" (sub-documento além da profundidade); tive %v`, out)
	}
	if !strings.Contains(got, "Boa Viagem") || strings.Contains(got, "\n") {
		t.Errorf("endereco.geo.detalhe.interno = %q, want JSON compacto contendo Boa Viagem", got)
	}
}

func TestMongoFlattenInto_ArraySempreJSONCompacto(t *testing.T) {
	doc := bson.M{
		"tags": bson.A{"a", "b", "c"},
	}
	out := map[string]string{}
	mongoFlattenInto(doc, "", mongoFlattenDepth, out)

	got, ok := out["tags"]
	if !ok {
		t.Fatal(`esperava a chave "tags"`)
	}
	if strings.Contains(got, "\n") {
		t.Errorf("tags = %q, esperava uma linha só (JSON compacto)", got)
	}
	if !strings.HasPrefix(got, "[") {
		t.Errorf("tags = %q, esperava um array JSON", got)
	}
}

func TestMongoChooseOrderField(t *testing.T) {
	t.Run("campo de data reconhecido vence", func(t *testing.T) {
		sample := bson.M{"created_at": bson.NewDateTimeFromTime(time.Now()), "nome": "x"}
		if got := mongoChooseOrderField(sample); got != "created_at" {
			t.Errorf("got %q, want created_at", got)
		}
	})
	t.Run("campo com nome de data mas tipo errado não conta", func(t *testing.T) {
		sample := bson.M{"created_at": "não é data de verdade"}
		if got := mongoChooseOrderField(sample); got != "" {
			t.Errorf("got %q, want vazio (tipo não é bson.DateTime)", got)
		}
	})
	t.Run("sem campo conhecido cai para _id (vazio)", func(t *testing.T) {
		sample := bson.M{"nome": "x"}
		if got := mongoChooseOrderField(sample); got != "" {
			t.Errorf("got %q, want vazio", got)
		}
	})
	t.Run("amostra nula cai para _id (vazio)", func(t *testing.T) {
		if got := mongoChooseOrderField(nil); got != "" {
			t.Errorf("got %q, want vazio", got)
		}
	})
}

func TestMongoRecentQuery(t *testing.T) {
	cases := []struct {
		name string
		d    mongoDescriptor
		want string
	}{
		{"com campo de data", mongoDescriptor{OrderField: "created_at"},
			"use loja; db.pedidos.find().sort({created_at: -1}).limit(20)"},
		{"sem campo de data (cai para _id)", mongoDescriptor{},
			"use loja; db.pedidos.find().sort({_id: -1}).limit(20)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mongoRecentQuery("loja", "pedidos", tc.d); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMongoFormatScalar(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
	}{
		{"nulo", nil, "∅"},
		{"string com quebra de linha", "linha1\nlinha2", "linha1 linha2"},
		{"objectid", mustMongoObjectIDFromHex(t, "507f1f77bcf86cd799439011"), "507f1f77bcf86cd799439011"},
		{"inteiro", int32(42), "42"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mongoFormatScalar(tc.v); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func mustMongoObjectIDFromHex(t *testing.T, hex string) bson.ObjectID {
	t.Helper()
	id, err := bson.ObjectIDFromHex(hex)
	if err != nil {
		t.Fatalf("bson.ObjectIDFromHex: %v", err)
	}
	return id
}

func TestMongoBuildResultSet_IdPrimeiroEColunasAusentesViramNulo(t *testing.T) {
	docs := []bson.M{
		{"_id": mustMongoObjectIDFromHex(t, "507f1f77bcf86cd799439011"), "nome": "Ana", "idade": int32(30)},
		{"_id": mustMongoObjectIDFromHex(t, "507f1f77bcf86cd799439012"), "nome": "Beto"}, // sem "idade"
	}
	rs := mongoBuildResultSet(docs, "consulta de teste", time.Millisecond)

	if rs.Columns[0] != "_id" {
		t.Fatalf("Columns[0] = %q, want _id", rs.Columns[0])
	}
	idadeIdx := -1
	for i, c := range rs.Columns {
		if c == "idade" {
			idadeIdx = i
		}
	}
	if idadeIdx < 0 {
		t.Fatalf("coluna idade não apareceu: %v", rs.Columns)
	}
	if rs.Rows[1][idadeIdx] != "∅" {
		t.Errorf("Rows[1][idade] = %q, want ∅ (documento sem esse campo)", rs.Rows[1][idadeIdx])
	}
	if rs.Language != "mongo" {
		t.Errorf("Language = %q, want mongo", rs.Language)
	}
}
