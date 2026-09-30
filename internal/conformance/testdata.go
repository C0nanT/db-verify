package conformance

// Sem build tag: os testes unitários (sem Docker) também leem os cabeçalhos
// de exemplo. O binário de produção não importa este pacote.

import (
	"os"
	"path/filepath"
	"testing"
)

// HeaderPath devolve o caminho de testdata/headers/<name>, na raiz do
// repositório, a partir de qualquer pacote: sobe do diretório corrente (que
// `go test` fixa no diretório do pacote) até achar o go.mod. Evita espalhar
// "../../../" pelos testes.
func HeaderPath(t testing.TB, name string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("diretório corrente: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "testdata", "headers", name)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod não encontrado subindo a partir do diretório corrente")
		}
		dir = parent
	}
}
