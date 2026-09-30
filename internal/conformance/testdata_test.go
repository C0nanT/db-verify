package conformance

import (
	"os"
	"testing"
)

// TestHeaderPath_AchaTestdataNaRaiz prova que o helper resolve os
// cabeçalhos da raiz mesmo rodando de dentro de internal/conformance.
func TestHeaderPath_AchaTestdataNaRaiz(t *testing.T) {
	p := HeaderPath(t, "plain.sql")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("HeaderPath(plain.sql) = %q, que não existe: %v", p, err)
	}
}
