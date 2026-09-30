package main

// Helpers pequenos compartilhados pelo main e pelas engines que ainda moram
// na raiz.

import "strings"

// printableStrings extrai as sequências imprimíveis (>=2 chars) de um blob binário.
func printableStrings(b []byte) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() >= 2 {
			out = append(out, cur.String())
		}
		cur.Reset()
	}
	for _, c := range b {
		if c >= 0x20 && c < 0x7f {
			cur.WriteByte(c)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// truncate corta s em w runas, terminando em "…" quando precisa cortar. Há
// uma cópia privada em internal/ui: o ui não pode ser importado pelas engines.
func truncate(s string, w int) string {
	r := []rune(s)
	if w <= 0 {
		return ""
	}
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}
