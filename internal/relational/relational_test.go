package relational

import "testing"

func TestChooseRelationalOrderColumn(t *testing.T) {
	dates := map[string]bool{"stamp": true}
	tests := []struct {
		name       string
		cols       []Column
		pk         string
		wantCol    string
		wantByDate bool
	}{
		{"empate na mesma camada: menor posição ordinal", []Column{{"inserted_at", "text"}, {"created_at", "text"}}, "id", "inserted_at", true},
		{"empate inverso: ordem manda, não o nome", []Column{{"created_at", "text"}, {"inserted_at", "text"}}, "id", "created_at", true},
		{"camada menor vence posição", []Column{{"updated_at", "stamp"}, {"created_at", "text"}}, "id", "created_at", true},
		{"tipo de data do dialeto", []Column{{"id", "int"}, {"quando", "stamp"}}, "id", "quando", true},
		{"tipo fora do conjunto do dialeto", []Column{{"quando", "datetime"}}, "id", "id", false},
		{"nome vence tipo de data", []Column{{"quando", "stamp"}, {"date", "text"}}, "", "date", true},
		{"fallback para PK", []Column{{"label", "text"}}, "id", "id", false},
		{"nenhuma coluna", []Column{{"label", "text"}}, "", "", false},
		{"sem colunas", nil, "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			col, byDate := ChooseOrderColumn(tc.cols, tc.pk, dates)
			if col != tc.wantCol || byDate != tc.wantByDate {
				t.Errorf("got (%q, %v), want (%q, %v)", col, byDate, tc.wantCol, tc.wantByDate)
			}
		})
	}
}
