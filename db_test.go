package main

// Testes de caracterização das partes de postgres.go que não dependem de um
// banco vivo: montagem do SQL de "recentes" (pgRecentQuery, antes
// TableInfo.RecentQuery), o nome de exibição de uma coleção
// (Collection.Qualified, antes TableInfo.Qualified) e formatação de valores
// (formatValue). A heurística de escolha da coluna de ordenação em si vive
// dentro de tablesSQL (uma query SQL, não Go) e por isso só pode ser
// caracterizada com um Postgres de verdade — ver docker_test.go.
//
// Estes testes eram originalmente escritos contra TableInfo; a extração da
// interface Engine/Session (ticket 02) trocou TableInfo por Collection +
// pgDescriptor, então as chamadas foram adaptadas — as asserções (os valores
// esperados) continuam as mesmas.

import (
	"testing"
	"time"
)

// TestPGRecentQuery caracteriza o SQL produzido para os 20 mais recentes,
// incluindo o fallback SELECT * ... LIMIT 20 quando não há coluna de
// ordenação.
func TestPGRecentQuery(t *testing.T) {
	cases := []struct {
		name      string
		namespace string
		coll      string
		d         pgDescriptor
		want      string
	}{
		{
			name:      "sem coluna de ordenação cai no fallback",
			namespace: "public", coll: "logs",
			want: `SELECT * FROM "public"."logs" LIMIT 20;`,
		},
		{
			name:      "com coluna de ordenação ordena decrescente",
			namespace: "public", coll: "posts", d: pgDescriptor{OrderCol: "created_at"},
			want: `SELECT * FROM "public"."posts" ORDER BY "created_at" DESC LIMIT 20;`,
		},
		{
			name:      "schema diferente de public",
			namespace: "app", coll: "usuarios", d: pgDescriptor{OrderCol: "id"},
			want: `SELECT * FROM "app"."usuarios" ORDER BY "id" DESC LIMIT 20;`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pgRecentQuery(tc.namespace, tc.coll, tc.d); got != tc.want {
				t.Errorf("pgRecentQuery() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCollection_Qualified caracteriza a concatenação namespace.nome.
func TestCollection_Qualified(t *testing.T) {
	c := Collection{Namespace: "public", Name: "pedidos"}
	if got, want := c.Qualified(), "public.pedidos"; got != want {
		t.Errorf("Qualified() = %q, want %q", got, want)
	}
}

// TestFormatValue caracteriza a formatação de valores usada para preencher
// o painel de resultados: nulo, date, timestamp, binário e string com
// quebra de linha.
func TestFormatValue(t *testing.T) {
	midnight := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	withTime := time.Date(2024, 3, 15, 13, 45, 30, 0, time.UTC)

	cases := []struct {
		name string
		in   any
		want string
	}{
		{"nulo", nil, "∅"},
		{"date (hora zerada vira só a data)", midnight, "2024-03-15"},
		{"timestamp com hora", withTime, "2024-03-15 13:45:30"},
		{"binário", []byte{0xde, 0xad, 0xbe, 0xef}, `\xdeadbeef`},
		{"string com quebra de linha é achatada", "linha um\nlinha dois\n\tlinha três", "linha um linha dois linha três"},
		{"string simples", "ok", "ok"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatValue(tc.in); got != tc.want {
				t.Errorf("formatValue(%#v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
