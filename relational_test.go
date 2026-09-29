package main

import "testing"

func TestChooseRelationalOrderColumn(t *testing.T) {
	dates := map[string]bool{"stamp": true}
	tests := []struct {
		name       string
		cols       []relationalColumn
		pk         string
		wantCol    string
		wantByDate bool
	}{
		{"empate na mesma camada: menor posição ordinal", []relationalColumn{{"inserted_at", "text"}, {"created_at", "text"}}, "id", "inserted_at", true},
		{"empate inverso: ordem manda, não o nome", []relationalColumn{{"created_at", "text"}, {"inserted_at", "text"}}, "id", "created_at", true},
		{"camada menor vence posição", []relationalColumn{{"updated_at", "stamp"}, {"created_at", "text"}}, "id", "created_at", true},
		{"tipo de data do dialeto", []relationalColumn{{"id", "int"}, {"quando", "stamp"}}, "id", "quando", true},
		{"tipo fora do conjunto do dialeto", []relationalColumn{{"quando", "datetime"}}, "id", "id", false},
		{"nome vence tipo de data", []relationalColumn{{"quando", "stamp"}, {"date", "text"}}, "", "date", true},
		{"fallback para PK", []relationalColumn{{"label", "text"}}, "id", "id", false},
		{"nenhuma coluna", []relationalColumn{{"label", "text"}}, "", "", false},
		{"sem colunas", nil, "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			col, byDate := chooseRelationalOrderColumn(tc.cols, tc.pk, dates)
			if col != tc.wantCol || byDate != tc.wantByDate {
				t.Errorf("got (%q, %v), want (%q, %v)", col, byDate, tc.wantCol, tc.wantByDate)
			}
		})
	}
}
