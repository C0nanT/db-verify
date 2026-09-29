package main

import (
	"strings"
	"testing"
)

func longResult() *ResultSet {
	long := strings.Repeat("x", 100) + "FIM"
	return &ResultSet{
		Columns: []string{"id", "payload"},
		Rows:    [][]string{{"1", long}, {"2", "curto"}},
	}
}

// Modo compacto trunca em maxCol; modo expandido mantém a célula inteira.
func TestColWidthsWide(t *testing.T) {
	rs := longResult()
	if got := colWidths(rs, false)[1]; got != maxCol {
		t.Fatalf("compacto: largura = %d, quer %d", got, maxCol)
	}
	if got := colWidths(rs, true)[1]; got != 103 {
		t.Fatalf("expandido: largura = %d, quer 103", got)
	}
}

// No modo expandido, rolar até o fim revela o final da célula longa.
func TestRenderTableWideRevealsEnd(t *testing.T) {
	rs := longResult()
	total := tableWidth(colWidths(rs, true))
	lines := renderTable(rs, 20, 20, total-20, true)
	if !strings.Contains(lines[2], "FIM") {
		t.Fatalf("fim da célula não visível: %q", lines[2])
	}
	compact := renderTable(rs, 200, 20, 0, false)
	if strings.Contains(compact[2], "FIM") {
		t.Fatalf("modo compacto deveria truncar: %q", compact[2])
	}
}

// Quebras de linha dentro da célula não podem quebrar a linha da tabela.
func TestRenderTableFlattensNewlines(t *testing.T) {
	rs := &ResultSet{Columns: []string{"txt"}, Rows: [][]string{{"a\nb\r\nc\td"}}}
	row := renderTable(rs, 80, 20, 0, true)[2]
	if strings.ContainsAny(row, "\r\n\t") || !strings.Contains(row, "a⏎b⏎c d") {
		t.Fatalf("célula não achatada: %q", row)
	}
}

// O scroll horizontal fica entre 0 e o fim da tabela, e acompanha o modo/painel.
func TestScrollByClamps(t *testing.T) {
	m := &model{width: 100, height: 30, res: longResult(), wide: true}
	m.scrollBy(1 << 30)
	want := tableWidth(colWidths(m.res, true)) - m.tableViewW()
	if m.hscroll != want {
		t.Fatalf("hscroll = %d, quer %d", m.hscroll, want)
	}
	m.scrollBy(-1 << 30)
	if m.hscroll != 0 {
		t.Fatalf("hscroll = %d, quer 0", m.hscroll)
	}

	m.noList = true
	if m.listW() != 0 || m.tableViewW() != m.width-6 {
		t.Fatalf("painel oculto: listW=%d tableViewW=%d", m.listW(), m.tableViewW())
	}
}
