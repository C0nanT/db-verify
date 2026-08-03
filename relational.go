package main

// Heurística de coluna de ordenação compartilhada entre as engines
// relacionais (Postgres, MySQL/MariaDB): nomes de criação vencem, depois
// publicação, depois atualização, depois qualquer coluna de data/timestamp,
// depois a PK simples da tabela — nessa ordem (ver SPEC.md, "Modelo de
// dados genérico"). Cada engine traduz esses mesmos nomes para o dialeto do
// seu information_schema; o Postgres monta a CASE WHEN em SQL a partir
// desta lista (ver postgres.go, tablesSQL), o MySQL decide em Go a partir
// dela (ver mysql.go, chooseOrderColumn) — mas os nomes vêm de um lugar só.

import (
	"fmt"
	"strings"
)

// orderColumnTiers é a lista de nomes de coluna considerados "data de
// criação"/"data de publicação"/"data de atualização", em ordem decrescente
// de preferência. Inclui os nomes em português já suportados hoje.
var orderColumnTiers = [][]string{
	{"created_at", "criado_em", "data_criacao", "data_cadastro", "date_created", "inserted_at", "date_joined"},
	{"published_at", "data_publicacao", "data", "date", "datahora", "timestamp"},
	{"updated_at", "atualizado_em", "data_atualizacao", "modified", "last_modified"},
}

// sqlStringList formata uma lista de nomes como literais separados por
// vírgula para uso dentro de um IN (...) SQL, ex.: 'a','b','c'. Os nomes vêm
// só de orderColumnTiers (constantes internas, nunca de entrada externa),
// então concatenação direta é segura aqui.
func sqlStringList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "'" + n + "'"
	}
	return strings.Join(quoted, ",")
}

// orderHint descreve, numa linha, como Recent escolheu ordenar uma coleção —
// usado pelo Hint que a TUI exibe. Compartilhado entre engines relacionais.
func orderHint(col string, byDate bool) string {
	if col == "" {
		return "sem coluna de ordenação"
	}
	kind := "PK"
	if byDate {
		kind = "data"
	}
	return fmt.Sprintf("ordenado por %s (%s)", col, kind)
}
