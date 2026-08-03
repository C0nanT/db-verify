package main

// Testes do picker de data/ (Camada 1): passa a listar todo arquivo
// reconhecido por alguma engine — não só .dump — com uma coluna mostrando
// qual engine foi detectada, e arquivos não reconhecidos aparecem marcados
// como desconhecidos em vez de sumirem da lista.
//
// findBackups usa o registro global (só Postgres hoje, que reconhece
// qualquer coisa por palpite — ver postgres.go), então neste repositório
// nenhum arquivo real cai em Unknown; o comportamento de "não selecionável"
// é testado diretamente contra pickerModel com uma entrada Unknown
// construída à mão, sem depender de uma segunda engine existir.

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestFindBackups_ListaTodoArquivoReconhecido cobre a mudança central do
// ticket: o picker não filtra mais por .dump/.dump.gz. Um .sql, um .dump e
// um .txt (que hoje cai no palpite do Postgres) aparecem todos, cada um com
// a engine detectada.
func TestFindBackups_ListaTodoArquivoReconhecido(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, "testdata/headers/custom.dump", filepath.Join(dir, "a.dump"))
	copyFixture(t, "testdata/headers/plain.sql", filepath.Join(dir, "b.sql"))
	copyFixture(t, "testdata/headers/random.txt", filepath.Join(dir, "c.txt"))

	backups, err := findBackups(dir)
	if err != nil {
		t.Fatalf("findBackups: %v", err)
	}
	if len(backups) != 3 {
		t.Fatalf("esperava 3 arquivos listados (não só .dump), tive %d: %+v", len(backups), backups)
	}
	for _, b := range backups {
		if b.Engine != "postgres" {
			t.Errorf("%s: Engine = %q, want postgres", b.Name, b.Engine)
		}
		if b.Unknown {
			t.Errorf("%s: não deveria estar marcado como desconhecido", b.Name)
		}
	}
}

// TestFindBackups_IgnoraDiretoriosEArquivosOcultos: findBackups não lista
// diretórios nem dotfiles.
func TestFindBackups_IgnoraDiretoriosEArquivosOcultos(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, "testdata/headers/plain.sql", filepath.Join(dir, "b.sql"))
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	copyFixture(t, "testdata/headers/plain.sql", filepath.Join(dir, ".hidden.sql"))

	backups, err := findBackups(dir)
	if err != nil {
		t.Fatalf("findBackups: %v", err)
	}
	if len(backups) != 1 || backups[0].Name != "b.sql" {
		t.Fatalf("esperava só b.sql, tive %+v", backups)
	}
}

// TestPickerModel_DesconhecidoNaoSelecionavel: pressionar enter sobre uma
// entrada Unknown não seleciona nada nem fecha a TUI — o operador não pode
// escolher um arquivo que nenhuma engine sabe provisionar.
func TestPickerModel_DesconhecidoNaoSelecionavel(t *testing.T) {
	m := newPickerModel([]backupEntry{
		{Path: "/data/misterioso.bin", Name: "misterioso.bin", Unknown: true},
	})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pm := updated.(*pickerModel)
	if pm.selected != "" {
		t.Errorf("esperava selected vazio para entrada Unknown, tive %q", pm.selected)
	}
	if pm.quitting {
		t.Errorf("esperava a TUI continuar aberta ao tentar selecionar um desconhecido")
	}
}

// TestPickerModel_ConhecidoESelecionavel: o mesmo enter, numa entrada
// reconhecida, seleciona e fecha.
func TestPickerModel_ConhecidoESelecionavel(t *testing.T) {
	m := newPickerModel([]backupEntry{
		{Path: "/data/dump.sql", Name: "dump.sql", Engine: "postgres"},
	})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pm := updated.(*pickerModel)
	if pm.selected != "/data/dump.sql" {
		t.Errorf("selected = %q, want /data/dump.sql", pm.selected)
	}
	if !pm.quitting {
		t.Errorf("esperava quitting=true após selecionar")
	}
}

// TestBackupEntry_EngineLabel caracteriza o rótulo exibido: nome da engine
// para reconhecidos, "desconhecido" explícito para o resto.
func TestBackupEntry_EngineLabel(t *testing.T) {
	known := backupEntry{Engine: "postgres"}
	if got := known.engineLabel(); got != "postgres" {
		t.Errorf("engineLabel() = %q, want postgres", got)
	}
	unknown := backupEntry{Unknown: true}
	if got := unknown.engineLabel(); got != "desconhecido" {
		t.Errorf("engineLabel() = %q, want desconhecido", got)
	}
}

func copyFixture(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("lendo fixture %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("escrevendo %s: %v", dst, err)
	}
}
