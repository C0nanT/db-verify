package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// dumpEntry é um arquivo .dump encontrado na pasta data/.
type dumpEntry struct {
	Path    string
	Name    string
	Size    int64
	ModTime string
}

// findDumps procura arquivos .dump (e .dump.gz) dentro de dir, sem recursão.
func findDumps(dir string) ([]dumpEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []dumpEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		low := strings.ToLower(name)
		if !strings.HasSuffix(low, ".dump") && !strings.HasSuffix(low, ".dump.gz") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, dumpEntry{
			Path:    filepath.Join(dir, name),
			Name:    name,
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04"),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// pickerModel é uma TUI simples de seleção de arquivo.
type pickerModel struct {
	dumps    []dumpEntry
	cursor   int
	selected string
	quitting bool
	width    int
}

func newPickerModel(dumps []dumpEntry) *pickerModel {
	return &pickerModel{dumps: dumps, width: 80}
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
			if m.cursor < len(m.dumps)-1 {
				m.cursor++
			}
		case "enter":
			m.selected = m.dumps[m.cursor].Path
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
	b.WriteString(stDim.Render("selecione um dump em data/ · ↑/↓ navega · enter confirma · q sai"))
	b.WriteString("\n\n")

	nameW := 0
	for _, d := range m.dumps {
		if len(d.Name) > nameW {
			nameW = len(d.Name)
		}
	}
	for i, d := range m.dumps {
		row := fmt.Sprintf("%-*s  %8s  %s", nameW, d.Name, humanSize(d.Size), d.ModTime)
		if i == m.cursor {
			row = stSel.Render("▸ " + row)
		} else {
			row = "  " + row
		}
		b.WriteString(row + "\n")
	}
	return stBox.Width(m.width - 2).Render(strings.Trim(b.String(), "\n"))
}

// pickDump lista os .dump em dataDir e pede ao usuário para escolher um.
// Se houver exatamente um arquivo, ainda assim exibe a seleção para confirmação.
func pickDump(dataDir string) (string, error) {
	dumps, err := findDumps(dataDir)
	if err != nil {
		return "", fmt.Errorf("não foi possível ler %s: %w", dataDir, err)
	}
	if len(dumps) == 0 {
		return "", fmt.Errorf("nenhum arquivo .dump encontrado em %s", dataDir)
	}

	m := newPickerModel(dumps)
	p := tea.NewProgram(m)
	res, err := p.Run()
	if err != nil {
		return "", err
	}
	final := res.(*pickerModel)
	if final.selected == "" {
		return "", fmt.Errorf("nenhum dump selecionado")
	}
	return final.selected, nil
}
