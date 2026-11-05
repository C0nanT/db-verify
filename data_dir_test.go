package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestChooseDataDir fixa a precedência da pasta de backups: ./data do diretório
// atual, depois data/ ao lado do binário. Sem nenhuma das duas, fica ./data, que é
// o caminho que faz sentido mostrar na mensagem de erro.
func TestChooseDataDir(t *testing.T) {
	mkdir := func(t *testing.T, path string) {
		t.Helper()
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("as duas existem: vence o diretório atual", func(t *testing.T) {
		local, beside := filepath.Join(t.TempDir(), "data"), filepath.Join(t.TempDir(), "data")
		mkdir(t, local)
		mkdir(t, beside)
		if got := chooseDataDir(local, beside); got != local {
			t.Fatalf("got %q, want %q", got, local)
		}
	})

	t.Run("só ao lado do binário: usa ela", func(t *testing.T) {
		local, beside := filepath.Join(t.TempDir(), "data"), filepath.Join(t.TempDir(), "data")
		mkdir(t, beside)
		if got := chooseDataDir(local, beside); got != beside {
			t.Fatalf("got %q, want %q", got, beside)
		}
	})

	t.Run("nenhuma existe: fica o diretório atual", func(t *testing.T) {
		local, beside := filepath.Join(t.TempDir(), "data"), filepath.Join(t.TempDir(), "data")
		if got := chooseDataDir(local, beside); got != local {
			t.Fatalf("got %q, want %q", got, local)
		}
	})

	t.Run("arquivo chamado data no diretório atual não conta", func(t *testing.T) {
		local, beside := filepath.Join(t.TempDir(), "data"), filepath.Join(t.TempDir(), "data")
		if err := os.WriteFile(local, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		mkdir(t, beside)
		if got := chooseDataDir(local, beside); got != beside {
			t.Fatalf("got %q, want %q", got, beside)
		}
	})
}
