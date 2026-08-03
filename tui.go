package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ------------------------------------------------------------------ estilos --

var (
	cPrimary = lipgloss.Color("212")
	cAccent  = lipgloss.Color("39")
	cOK      = lipgloss.Color("42")
	cWarn    = lipgloss.Color("214")
	cErr     = lipgloss.Color("203")
	cDim     = lipgloss.Color("244")
	cSelBg   = lipgloss.Color("57")

	stTitle  = lipgloss.NewStyle().Bold(true).Foreground(cPrimary)
	stLabel  = lipgloss.NewStyle().Foreground(cDim)
	stValue  = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	stOK     = lipgloss.NewStyle().Foreground(cOK).Bold(true)
	stWarn   = lipgloss.NewStyle().Foreground(cWarn).Bold(true)
	stErr    = lipgloss.NewStyle().Foreground(cErr).Bold(true)
	stDim    = lipgloss.NewStyle().Foreground(cDim)
	stAccent = lipgloss.NewStyle().Foreground(cAccent)
	stSel    = lipgloss.NewStyle().Background(cSelBg).Foreground(lipgloss.Color("231")).Bold(true)
	stColHdr = lipgloss.NewStyle().Foreground(cAccent).Bold(true).Underline(true)

	stBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cDim).Padding(0, 1)
)

// ------------------------------------------------------------------ modelo ---

type queryDoneMsg struct {
	coll Collection
	res  *ResultSet
	err  error
}

// model é agnóstico de engine: fala só com Session e os tipos genéricos
// (Backup, Health, Collection, ResultSet). Nenhuma referência a Postgres,
// pgx, SQL ou "schema.tabela" vive aqui.
type model struct {
	sess           Session
	backup         *Backup
	hint           ConnectHint
	restore        *RestoreResult
	health         *Health
	allCollections []Collection

	collections []Collection // após filtro
	cursor      int
	offset      int
	filter      string
	filtOn      bool
	loading     bool

	res     *ResultSet
	resFor  string
	resErr  error
	hscroll int

	width, height int
	quitting      bool
}

const (
	headerLines = 5 // altura do cabeçalho (3 linhas + bordas)
	footerLines = 1
	nameGutter  = 18 // espaço reservado para LINHAS + TAMANHO
)

// listW: largura do painel de coleções, adaptada ao terminal.
func (m *model) listW() int {
	w := m.width / 3
	if w > 56 {
		w = 56
	}
	if w < 40 {
		w = 40
	}
	return w
}

func newModel(sess Session, backup *Backup, health *Health, collections []Collection) *model {
	m := &model{
		sess: sess, backup: backup, hint: sess.ConnectHint(), restore: sess.Restore(), health: health,
		allCollections: collections, collections: collections, width: 100, height: 30,
	}
	return m
}

func (m *model) Init() tea.Cmd { return m.loadCurrent() }

// loadCurrent dispara a consulta dos 20 mais recentes da coleção sob o cursor.
func (m *model) loadCurrent() tea.Cmd {
	if len(m.collections) == 0 {
		return nil
	}
	c := m.collections[m.cursor]
	if c.Qualified() == m.resFor && m.res != nil {
		return nil
	}
	m.loading = true
	return func() tea.Msg {
		res, err := m.sess.Recent(context.Background(), c)
		return queryDoneMsg{coll: c, res: res, err: err}
	}
}

func (m *model) bodyHeight() int {
	h := m.height - headerLines - footerLines
	if h < 5 {
		h = 5
	}
	return h
}

// visibleRows = linhas que cabem na lista (descontando bordas, cabeçalho e rodapé).
func (m *model) visibleRows() int {
	n := m.bodyHeight() - 4
	if n < 1 {
		n = 1
	}
	return n
}

// listRowY converte índice da lista em linha absoluta da tela (para o mouse).
func (m *model) rowAtY(y int) (int, bool) {
	first := headerLines + 2 // borda superior + cabeçalho de colunas
	idx := m.offset + (y - first)
	if y < first || idx < 0 || idx >= len(m.collections) || idx-m.offset >= m.visibleRows() {
		return 0, false
	}
	return idx, true
}

func (m *model) clampCursor() {
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.collections) {
		m.cursor = len(m.collections) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	vis := m.visibleRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+vis {
		m.offset = m.cursor - vis + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *model) applyFilter() {
	if m.filter == "" {
		m.collections = m.allCollections
	} else {
		f := strings.ToLower(m.filter)
		m.collections = nil
		for _, c := range m.allCollections {
			if strings.Contains(strings.ToLower(c.Qualified()), f) {
				m.collections = append(m.collections, c)
			}
		}
	}
	m.cursor, m.offset = 0, 0
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampCursor()

	case queryDoneMsg:
		m.loading = false
		m.res, m.resErr, m.resFor, m.hscroll = msg.res, msg.err, msg.coll.Qualified(), 0

	case tea.MouseMsg:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if msg.X < m.listW() {
				m.cursor--
				m.clampCursor()
				return m, m.loadCurrent()
			}
		case tea.MouseButtonWheelDown:
			if msg.X < m.listW() {
				m.cursor++
				m.clampCursor()
				return m, m.loadCurrent()
			}
		case tea.MouseButtonLeft:
			if msg.X < m.listW() {
				if idx, ok := m.rowAtY(msg.Y); ok {
					m.cursor = idx
					m.clampCursor()
					return m, m.loadCurrent()
				}
			}
		}

	case tea.KeyMsg:
		// terminais podem entregar várias teclas de uma vez (colagem, buffer):
		// processa uma a uma para não perder comandos.
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 {
			var cmds []tea.Cmd
			for _, r := range msg.Runes {
				_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			return m, tea.Batch(cmds...)
		}

		// modo filtro: digita para filtrar a lista
		if m.filtOn {
			switch msg.Type {
			case tea.KeyEsc:
				m.filtOn, m.filter = false, ""
				m.applyFilter()
				return m, m.loadCurrent()
			case tea.KeyEnter, tea.KeyCtrlJ: // CR ou LF
				m.filtOn = false
				return m, m.loadCurrent()
			case tea.KeyBackspace:
				if len(m.filter) > 0 {
					m.filter = m.filter[:len(m.filter)-1]
					m.applyFilter()
				}
				return m, nil
			case tea.KeyRunes, tea.KeySpace:
				m.filter += string(msg.Runes)
				m.applyFilter()
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			m.cursor--
			m.clampCursor()
			return m, m.loadCurrent()
		case "down", "j":
			m.cursor++
			m.clampCursor()
			return m, m.loadCurrent()
		case "pgup":
			m.cursor -= m.visibleRows()
			m.clampCursor()
			return m, m.loadCurrent()
		case "pgdown":
			m.cursor += m.visibleRows()
			m.clampCursor()
			return m, m.loadCurrent()
		case "home", "g":
			m.cursor = 0
			m.clampCursor()
			return m, m.loadCurrent()
		case "end", "G":
			m.cursor = len(m.collections) - 1
			m.clampCursor()
			return m, m.loadCurrent()
		case "enter", "ctrl+j", "r":
			m.resFor = "" // força reexecutar
			return m, m.loadCurrent()
		case "left", "h":
			m.hscroll -= 8
			if m.hscroll < 0 {
				m.hscroll = 0
			}
		case "right", "l":
			m.hscroll += 8
		case "/":
			m.filtOn, m.filter = true, ""
			m.applyFilter()
		}
	}
	return m, nil
}

// ------------------------------------------------------------------- view ----

func (m *model) View() string {
	if m.quitting {
		return ""
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.viewList(), m.viewResult())
	return m.viewHeader() + "\n" + body + "\n" + m.viewFooter()
}

func (m *model) viewHeader() string {
	status := stOK.Render("✓ restore sem erros")
	if m.restore != nil && len(m.restore.Errors) > 0 {
		status = stWarn.Render(fmt.Sprintf("! %d erro(s) no restore", len(m.restore.Errors)))
	}
	origin := m.backup.OriginDB
	if origin == "" {
		origin = "?"
	}
	l1 := fmt.Sprintf("%s  %s  %s",
		stTitle.Render("Verify Backup"),
		stDim.Render("·"),
		stValue.Render(shortPath(m.backup.Path, m.width-24)))
	l2 := fmt.Sprintf("%s %s   %s %s   %s %s   %s %s   %s",
		stLabel.Render("origem:"), stValue.Render(origin),
		stLabel.Render("engine:"), stValue.Render(m.backup.Engine+" "+orDash(m.backup.Version)),
		stLabel.Render("formato:"), stValue.Render(m.backup.Format),
		stLabel.Render("backup:"), stValue.Render(humanSize(m.backup.Size)),
		status)

	fields := []string{fmt.Sprintf("%s %s", stLabel.Render("tamanho:"), stValue.Render(m.health.Size))}
	for _, f := range m.health.Fields {
		fields = append(fields, fmt.Sprintf("%s %s", stLabel.Render(f.Label+":"), stValue.Render(f.Value)))
	}
	if m.hint.Port != 0 {
		fields = append(fields, fmt.Sprintf("%s %s", stLabel.Render("porta:"), stAccent.Render(fmt.Sprint(m.hint.Port))))
	}
	l3 := strings.Join(fields, "   ")

	return stBox.Width(m.width - 2).Render(strings.Join([]string{l1, l2, l3}, "\n"))
}

func (m *model) viewList() string {
	inner := m.listW() - 4
	nameW := inner - nameGutter
	title := fmt.Sprintf("%-*s %7s %9s", nameW, "TABELA", "LINHAS", "TAMANHO")
	lines := []string{stColHdr.Render(title)}

	vis := m.visibleRows()
	for i := m.offset; i < len(m.collections) && i < m.offset+vis; i++ {
		c := m.collections[i]
		name := truncate(c.Qualified(), nameW)
		row := fmt.Sprintf("%-*s %7d %9s", nameW, name, c.Count, shortSize(c.Size))
		switch {
		case i == m.cursor:
			row = stSel.Render(row)
		case c.Count == 0:
			row = stDim.Render(row)
		}
		lines = append(lines, row)
	}
	for len(lines) < vis+1 {
		lines = append(lines, "")
	}
	if m.filtOn || m.filter != "" {
		lines = append(lines, stAccent.Render("/"+m.filter+"▌"))
	} else {
		lines = append(lines, stDim.Render(fmt.Sprintf("%d tabelas", len(m.collections))))
	}
	return stBox.Width(m.listW() - 2).Height(m.bodyHeight() - 2).Render(strings.Join(lines, "\n"))
}

func (m *model) viewResult() string {
	w := m.width - m.listW() - 2
	if w < 20 {
		w = 20
	}
	h := m.bodyHeight() - 2

	if len(m.collections) == 0 {
		return stBox.Width(w - 2).Height(h).Render(stDim.Render("nenhuma tabela"))
	}
	c := m.collections[m.cursor]

	head := stTitle.Render("20 mais recentes · " + c.Qualified())
	hint := stDim.Render(c.Hint)

	queryText := c.Preview
	if m.res != nil && m.resFor == c.Qualified() {
		queryText = m.res.Query
	}
	queryLine := stAccent.Render(truncate(queryText, w-4))

	var bodyLines []string
	switch {
	case m.loading:
		bodyLines = []string{stDim.Render("consultando…")}
	case m.resErr != nil:
		bodyLines = []string{stErr.Render("erro: " + m.resErr.Error())}
	case m.res == nil || len(m.res.Rows) == 0:
		bodyLines = []string{stWarn.Render("tabela vazia — nenhum registro")}
	default:
		bodyLines = renderTable(m.res, w-4, h-4, m.hscroll)
	}
	lines := append([]string{head, hint, queryLine, ""}, bodyLines...)
	return stBox.Width(w - 2).Height(h).Render(strings.Join(lines, "\n"))
}

func (m *model) viewFooter() string {
	keys := []string{"↑/↓ navegar", "clique/enter consultar", "←/→ rolar colunas", "/ filtrar", "r recarregar", "q sair"}
	return stDim.Render("  " + strings.Join(keys, "  ·  "))
}

// renderTable desenha o resultset em colunas alinhadas, com scroll horizontal.
func renderTable(rs *ResultSet, width, maxRows, hscroll int) []string {
	const maxCol = 28
	widths := make([]int, len(rs.Columns))
	for i, c := range rs.Columns {
		widths[i] = len([]rune(c))
	}
	for _, row := range rs.Rows {
		for i, v := range row {
			if n := len([]rune(v)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	for i := range widths {
		if widths[i] > maxCol {
			widths[i] = maxCol
		}
	}

	build := func(cells []string) string {
		var b strings.Builder
		for i, c := range cells {
			if i > 0 {
				b.WriteString(" │ ")
			}
			b.WriteString(pad(truncate(c, widths[i]), widths[i]))
		}
		return b.String()
	}

	header := build(rs.Columns)
	sep := strings.Repeat("─", len([]rune(header)))

	out := []string{
		stColHdr.Render(slice(header, hscroll, width)),
		stDim.Render(slice(sep, hscroll, width)),
	}
	for i, row := range rs.Rows {
		if i >= maxRows-3 {
			out = append(out, stDim.Render(fmt.Sprintf("… +%d linhas (janela pequena)", len(rs.Rows)-i)))
			break
		}
		out = append(out, slice(build(row), hscroll, width))
	}
	out = append(out, "", stDim.Render(fmt.Sprintf("%d linha(s) em %s", len(rs.Rows), rs.Elapsed.Round(1e6))))
	return out
}

// ------------------------------------------------------------------ utils ----

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

func pad(s string, w int) string {
	if n := len([]rune(s)); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

func slice(s string, from, width int) string {
	r := []rune(s)
	if from >= len(r) {
		return ""
	}
	r = r[from:]
	if len(r) > width {
		r = r[:width]
	}
	return string(r)
}

// shortSize encurta "8192 bytes" para "8192 B" e cabe na coluna.
func shortSize(s string) string {
	return truncate(strings.Replace(s, " bytes", " B", 1), 9)
}

func shortPath(p string, w int) string {
	if w < 20 {
		w = 20
	}
	if len(p) <= w {
		return p
	}
	return "…" + p[len(p)-w+1:]
}
