package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// backupEntry é um arquivo encontrado na pasta data/, já com a engine
// detectada (ou marcado como desconhecido, quando nenhuma engine reconhece
// o conteúdo).
type backupEntry struct {
	Path    string
	Name    string
	Size    int64
	ModTime string
	Engine  string // nome da engine detectada; "" quando desconhecido
	Unknown bool
}

// findBackups lista, sem recursão, todo arquivo regular de dir e roda a
// detecção de formato (só o cabeçalho, barato) em cada um. Arquivos que
// nenhuma engine reconhece entram mesmo assim, marcados como Unknown — o
// operador precisa ver que existe algo ali que a ferramenta não entende, em
// vez de o arquivo simplesmente sumir da lista.
func findBackups(dir string) ([]backupEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []backupEntry
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, e.Name())
		be := backupEntry{
			Path:    path,
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04"),
		}
		if backup, err := InspectDump(path); err == nil {
			be.Engine = backup.Engine
		} else {
			be.Unknown = true
		}
		out = append(out, be)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// pickerModel é uma TUI simples de seleção de arquivo.
type pickerModel struct {
	backups  []backupEntry
	cursor   int
	selected string
	quitting bool
	width    int
}

func newPickerModel(backups []backupEntry) *pickerModel {
	return &pickerModel{backups: backups, width: 80}
}

func (m *pickerModel) Init() tea.Cmd { return nil }

func (m *pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.backups)-1 {
				m.cursor++
			}
		case "enter":
			// arquivos não reconhecidos não são selecionáveis: nenhuma
			// engine sabe provisioná-los.
			if m.backups[m.cursor].Unknown {
				return m, nil
			}
			m.selected = m.backups[m.cursor].Path
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *pickerModel) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder
	b.WriteString(stTitle.Render("Verify Backup"))
	b.WriteString("\n")
	b.WriteString(stDim.Render("selecione um backup em data/ · ↑/↓ navega · enter confirma · q sai"))
	b.WriteString("\n\n")

	nameW, engineW := 0, len("engine")
	for _, d := range m.backups {
		if len(d.Name) > nameW {
			nameW = len(d.Name)
		}
		if len(d.engineLabel()) > engineW {
			engineW = len(d.engineLabel())
		}
	}
	for i, d := range m.backups {
		engine := d.engineLabel()
		if d.Unknown {
			engine = stDim.Render(fmt.Sprintf("%-*s", engineW, engine))
		} else {
			engine = stAccent.Render(fmt.Sprintf("%-*s", engineW, engine))
		}
		row := fmt.Sprintf("%-*s  %s  %8s  %s", nameW, d.Name, engine, humanSize(d.Size), d.ModTime)
		if d.Unknown {
			row = stDim.Render("  " + row)
		} else if i == m.cursor {
			row = stSel.Render("▸ " + row)
		} else {
			row = "  " + row
		}
		b.WriteString(row + "\n")
	}
	return stBox.Width(m.width - 2).Render(strings.Trim(b.String(), "\n"))
}

// engineLabel devolve o nome da engine detectada, ou um rótulo explícito de
// "desconhecido" para arquivos que nenhuma engine reconheceu.
func (d backupEntry) engineLabel() string {
	if d.Unknown {
		return "desconhecido"
	}
	return d.Engine
}

// pickDump lista os arquivos reconhecidos em dataDir e pede ao usuário para
// escolher um. Se houver exatamente um arquivo, ainda assim exibe a seleção
// para confirmação.
func pickDump(dataDir string) (string, error) {
	backups, err := findBackups(dataDir)
	if err != nil {
		return "", fmt.Errorf("não foi possível ler %s: %w", dataDir, err)
	}
	if len(backups) == 0 {
		return "", fmt.Errorf("nenhum arquivo de backup encontrado em %s", dataDir)
	}

	m := newPickerModel(backups)
	p := tea.NewProgram(m)
	res, err := p.Run()
	if err != nil {
		return "", err
	}
	final := res.(*pickerModel)
	if final.selected == "" {
		return "", fmt.Errorf("nenhum backup selecionado")
	}
	return final.selected, nil
}
