// Package relational reúne a heurística de coluna de ordenação e os helpers de
// dialeto compartilhados pelas engines relacionais. Importa no máximo o
// pacote engine.
package relational

// Heurística de coluna de ordenação compartilhada entre as engines
// relacionais: nomes de criação vencem, depois
// publicação, depois atualização, depois qualquer coluna de data/timestamp,
// depois a PK simples da tabela — nessa ordem (ver SPEC.md, "Modelo de
// dados genérico"). Cada engine traduz esses mesmos nomes para o seu
// dialeto; a lista de nomes e a decisão em Go moram aqui (o Postgres
// replica a decisão em SQL a partir da mesma lista).
// Consumidores:
//   - Postgres: monta a CASE WHEN em SQL a partir de OrderColumnTiers (ver
//     engines/postgres/postgres.go, tablesSQL);
//   - MySQL/MariaDB e SQLite: decidem em Go via ChooseOrderColumn,
//     cada um informando o próprio conjunto de tipos de data;
//   - Mongo: usa só as camadas 1–3 de nome, achatadas (ver engines/mongo/mongo.go).

import (
	"fmt"
	"strings"
)

// OrderColumnTiers é a lista de nomes de coluna considerados "data de
// criação"/"data de publicação"/"data de atualização", em ordem decrescente
// de preferência. Inclui os nomes em português já suportados hoje.
var OrderColumnTiers = [][]string{
	{"created_at", "criado_em", "data_criacao", "data_cadastro", "date_created", "inserted_at", "date_joined"},
	{"published_at", "data_publicacao", "data", "date", "datahora", "timestamp"},
	{"updated_at", "atualizado_em", "data_atualizacao", "modified", "last_modified"},
}

// Preferências devolvidas por columnPref: 1..3 são as camadas de
// nome de OrderColumnTiers.
const (
	// prefAnyDate é a camada de "qualquer coluna de data/hora", quando nenhum
	// nome conhecido casa.
	prefAnyDate = 4
	// prefNone marca a coluna que não é candidata a ordenação por data.
	prefNone = 9
)

// Column é uma coluna vista pela heurística, neutra de dialeto:
// nome e tipo de dado como o information_schema/PRAGMA do engine informa.
type Column struct {
	Name     string
	DataType string
}

// columnPref devolve a preferência da coluna: 1..3 para os nomes
// de OrderColumnTiers, prefAnyDate para qualquer coluna cujo tipo esteja em
// dateTypes (informado pelo dialeto), prefNone para o resto.
func columnPref(name, dataType string, dateTypes map[string]bool) int {
	for i, tier := range OrderColumnTiers {
		for _, n := range tier {
			if name == n {
				return i + 1
			}
		}
	}
	if dateTypes[dataType] {
		return prefAnyDate
	}
	return prefNone
}

// ChooseOrderColumn aplica a heurística às colunas de uma tabela,
// dadas na ordem ordinal: a de menor preferência vence e, em empate na mesma
// camada, vence a de menor posição ordinal (a primeira da lista, pois só uma
// preferência estritamente menor a substitui). Sem candidata a data, cai
// para a PK simples (pk, "" se não houver); sem nenhuma das duas, a tabela
// não tem coluna de ordenação.
func ChooseOrderColumn(cols []Column, pk string, dateTypes map[string]bool) (orderCol string, byDate bool) {
	best := prefNone
	for _, c := range cols {
		if p := columnPref(c.Name, c.DataType, dateTypes); p < best {
			best, orderCol = p, c.Name
		}
	}
	if orderCol != "" {
		return orderCol, best <= prefAnyDate
	}
	return pk, false
}

// SQLStringList formata uma lista de nomes como literais separados por
// vírgula para uso dentro de um IN (...) SQL, ex.: 'a','b','c'. Os nomes vêm
// só de OrderColumnTiers (constantes internas, nunca de entrada externa),
// então concatenação direta é segura aqui.
func SQLStringList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "'" + n + "'"
	}
	return strings.Join(quoted, ",")
}

// OrderHint descreve, numa linha, como Recent escolheu ordenar uma coleção —
// usado pelo Hint que a TUI exibe. Compartilhado entre engines relacionais.
func OrderHint(col string, byDate bool) string {
	if col == "" {
		return "sem coluna de ordenação"
	}
	kind := "PK"
	if byDate {
		kind = "data"
	}
	return fmt.Sprintf("ordenado por %s (%s)", col, kind)
}
