package ui

import (
	"context"
	"db-verify/internal/engine"
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

	StTitle  = lipgloss.NewStyle().Bold(true).Foreground(cPrimary)
	StLabel  = lipgloss.NewStyle().Foreground(cDim)
	stValue  = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	StOK     = lipgloss.NewStyle().Foreground(cOK).Bold(true)
	StWarn   = lipgloss.NewStyle().Foreground(cWarn).Bold(true)
	StErr    = lipgloss.NewStyle().Foreground(cErr).Bold(true)
	StDim    = lipgloss.NewStyle().Foreground(cDim)
	StAccent = lipgloss.NewStyle().Foreground(cAccent)
	stSel    = lipgloss.NewStyle().Background(cSelBg).Foreground(lipgloss.Color("231")).Bold(true)
	stColHdr = lipgloss.NewStyle().Foreground(cAccent).Bold(true).Underline(true)

	stBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cDim).Padding(0, 1)
)

// ------------------------------------------------------------------ modelo ---

type queryDoneMsg struct {
	coll engine.Collection
	res  *engine.ResultSet
	err  error
}

// model é agnóstico de engine: fala só com Session e os tipos genéricos
// (Backup, Health, Collection, ResultSet). Nenhuma referência a Postgres,
// pgx, SQL ou "schema.tabela" vive aqui.
type model struct {
	sess           engine.Session
	backup         *engine.Backup
	hint           engine.ConnectHint
	restore        *engine.RestoreResult
	health         *engine.Health
	allCollections []engine.Collection

	collections []engine.Collection // após filtro
	cursor      int
	offset      int
	filter      string
	filtOn      bool
	loading     bool

	res     *engine.ResultSet
	resFor  string
	resErr  error
	hscroll int
	wide    bool // colunas sem limite de largura: rola até ler a célula inteira
	noList  bool // painel de tabelas oculto: resultado usa a largura toda

	width, height int
	quitting      bool
}

const (
	headerLines = 5 // altura do cabeçalho (3 linhas + bordas)
	footerLines = 1
	nameGutter  = 18 // espaço reservado para LINHAS + TAMANHO
)

// listW: largura do painel de coleções, adaptada ao terminal (0 se oculto).
func (m *model) listW() int {
	if m.noList {
		return 0
	}
	w := m.width / 3
	if w > 56 {
		w = 56
	}
	if w < 40 {
		w = 40
	}
	return w
}

func NewModel(sess engine.Session, backup *engine.Backup, health *engine.Health, collections []engine.Collection) *model {
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
			m.scrollBy(-8)
		case "right", "l":
			m.scrollBy(8)
		case "shift+left", "H":
			m.scrollBy(-m.tableViewW())
		case "shift+right", "L":
			m.scrollBy(m.tableViewW())
		case "0":
			m.hscroll = 0
		case "$":
			m.scrollBy(1 << 30)
		case "e":
			m.wide = !m.wide
			m.scrollBy(0)
		case "tab":
			m.noList = !m.noList
			m.scrollBy(0)
		case "/":
			m.filtOn, m.filter = true, ""
			m.applyFilter()
		}
	}
	return m, nil
}

// ------------------------------------------------------------------- view ----

// resultW: largura externa do painel de resultado.
func (m *model) resultW() int {
	w := m.width - m.listW() - 2
	if w < 20 {
		w = 20
	}
	return w
}

// tableViewW: largura visível da tabela dentro do painel de resultado.
func (m *model) tableViewW() int { return m.resultW() - 4 }

// scrollBy desloca o scroll horizontal e o mantém entre o início e o fim da tabela.
func (m *model) scrollBy(d int) {
	m.hscroll += d
	limit := 0
	if m.res != nil {
		limit = tableWidth(colWidths(m.res, m.wide)) - m.tableViewW()
	}
	if m.hscroll > limit {
		m.hscroll = limit
	}
	if m.hscroll < 0 {
		m.hscroll = 0
	}
}

func (m *model) View() string {
	if m.quitting {
		return ""
	}
	body := m.viewResult()
	if !m.noList {
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.viewList(), body)
	}
	return m.viewHeader() + "\n" + body + "\n" + m.viewFooter()
}

func (m *model) viewHeader() string {
	status := StOK.Render("✓ restore sem erros")
	if m.restore != nil && len(m.restore.Errors) > 0 {
		status = StWarn.Render(fmt.Sprintf("! %d erro(s) no restore", len(m.restore.Errors)))
	}
	origin := m.backup.OriginDB
	if origin == "" {
		origin = "?"
	}
	l1 := fmt.Sprintf("%s  %s  %s",
		StTitle.Render("Verify Backup"),
		StDim.Render("·"),
		stValue.Render(shortPath(m.backup.Path, m.width-24)))
	l2 := fmt.Sprintf("%s %s   %s %s   %s %s   %s %s   %s",
		StLabel.Render("origem:"), stValue.Render(origin),
		StLabel.Render("engine:"), stValue.Render(m.backup.Engine+" "+OrDash(m.backup.Version)),
		StLabel.Render("formato:"), stValue.Render(m.backup.Format),
		StLabel.Render("backup:"), stValue.Render(engine.HumanSize(m.backup.Size)),
		status)

	fields := []string{fmt.Sprintf("%s %s", StLabel.Render("tamanho:"), stValue.Render(m.health.Size))}
	for _, f := range m.health.Fields {
		fields = append(fields, fmt.Sprintf("%s %s", StLabel.Render(f.Label+":"), stValue.Render(f.Value)))
	}
	if m.hint.Port != 0 {
		fields = append(fields, fmt.Sprintf("%s %s", StLabel.Render("porta:"), StAccent.Render(fmt.Sprint(m.hint.Port))))
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
			row = StDim.Render(row)
		}
		lines = append(lines, row)
	}
	for len(lines) < vis+1 {
		lines = append(lines, "")
	}
	if m.filtOn || m.filter != "" {
		lines = append(lines, StAccent.Render("/"+m.filter+"▌"))
	} else {
		lines = append(lines, StDim.Render(fmt.Sprintf("%d tabelas", len(m.collections))))
	}
	return stBox.Width(m.listW() - 2).Height(m.bodyHeight() - 2).Render(strings.Join(lines, "\n"))
}

func (m *model) viewResult() string {
	w := m.resultW()
	h := m.bodyHeight() - 2

	if len(m.collections) == 0 {
		return stBox.Width(w - 2).Height(h).Render(StDim.Render("nenhuma tabela"))
	}
	c := m.collections[m.cursor]

	head := StTitle.Render("20 mais recentes · " + c.Qualified())
	hint := StDim.Render(c.Hint)

	queryText := c.Preview
	if m.res != nil && m.resFor == c.Qualified() {
		queryText = m.res.Query
	}
	queryLine := StAccent.Render(truncate(queryText, w-4))

	var bodyLines []string
	switch {
	case m.loading:
		bodyLines = []string{StDim.Render("consultando…")}
	case m.resErr != nil:
		bodyLines = []string{StErr.Render("erro: " + m.resErr.Error())}
	case m.res == nil || len(m.res.Rows) == 0:
		bodyLines = []string{StWarn.Render("tabela vazia — nenhum registro")}
	default:
		bodyLines = renderTable(m.res, m.tableViewW(), h-4, m.hscroll, m.wide)
	}
	lines := append([]string{head, hint, queryLine, ""}, bodyLines...)
	return stBox.Width(w - 2).Height(h).Render(strings.Join(lines, "\n"))
}

func (m *model) viewFooter() string {
	expand := "e expandir colunas"
	if m.wide {
		expand = "e compactar colunas"
	}
	keys := []string{"↑/↓ navegar", "clique/enter consultar", "←/→ rolar (shift: tela)", expand, "tab ocultar lista", "/ filtrar", "r recarregar", "q sair"}
	return StDim.Render("  " + strings.Join(keys, "  ·  "))
}

// maxCol: largura máxima de uma coluna no modo compacto (fora do modo expandido).
const maxCol = 28

// colSep separa as colunas renderizadas.
const colSep = " │ "

// cellText achata a célula numa linha só: quebras e tabs quebrariam o layout da tabela.
var cellText = strings.NewReplacer("\r\n", "⏎", "\n", "⏎", "\r", "⏎", "\t", " ").Replace

// colWidths calcula a largura de cada coluna; wide=false limita cada uma a maxCol.
func colWidths(rs *engine.ResultSet, wide bool) []int {
	widths := make([]int, len(rs.Columns))
	for i, c := range rs.Columns {
		widths[i] = len([]rune(c))
	}
	for _, row := range rs.Rows {
		for i, v := range row {
			if n := len([]rune(cellText(v))); i < len(widths) && n > widths[i] {
				widths[i] = n
			}
		}
	}
	if !wide {
		for i := range widths {
			if widths[i] > maxCol {
				widths[i] = maxCol
			}
		}
	}
	return widths
}

// tableWidth: largura total de uma linha da tabela, com separadores.
func tableWidth(widths []int) int {
	total := 0
	for i, w := range widths {
		if i > 0 {
			total += len([]rune(colSep))
		}
		total += w
	}
	return total
}

// renderTable desenha o resultset em colunas alinhadas, com scroll horizontal.
// wide=true não trunca células: o conteúdo inteiro fica acessível via hscroll.
func renderTable(rs *engine.ResultSet, width, maxRows, hscroll int, wide bool) []string {
	widths := colWidths(rs, wide)

	build := func(cells []string) string {
		var b strings.Builder
		for i, c := range cells {
			if i >= len(widths) {
				break
			}
			if i > 0 {
				b.WriteString(colSep)
			}
			b.WriteString(pad(truncate(cellText(c), widths[i]), widths[i]))
		}
		return b.String()
	}

	header := build(rs.Columns)
	sep := strings.Repeat("─", len([]rune(header)))

	out := []string{
		stColHdr.Render(slice(header, hscroll, width)),
		StDim.Render(slice(sep, hscroll, width)),
	}
	for i, row := range rs.Rows {
		if i >= maxRows-3 {
			out = append(out, StDim.Render(fmt.Sprintf("… +%d linhas (janela pequena)", len(rs.Rows)-i)))
			break
		}
		out = append(out, slice(build(row), hscroll, width))
	}
	status := fmt.Sprintf("%d linha(s) em %s", len(rs.Rows), rs.Elapsed.Round(1e6))
	if total := tableWidth(widths); total > width {
		status += fmt.Sprintf("  ·  colunas %d–%d de %d", hscroll+1, min(hscroll+width, total), total)
	}
	out = append(out, "", StDim.Render(status))
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

// OrDash devolve "desconhecida" quando s é vazio; o main também a usa no
// resumo impresso antes da TUI.
func OrDash(s string) string {
	if s == "" {
		return "desconhecida"
	}
	return s
}
