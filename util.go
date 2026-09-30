package main

// Helpers pequenos do main.

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
