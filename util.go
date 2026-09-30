package main

// Helpers pequenos compartilhados pelas engines e pela interface que ainda
// moram na raiz.

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
