// Package tui implements the terminal interface and its write actions.
package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lucasarakaki/phpman/internal/core"
	"github.com/lucasarakaki/phpman/internal/detect"
)

type tab int

const (
	tabOverview tab = iota
	tabVersions
	tabDoctor
	tabCount
)

type loadedMsg struct {
	id   int
	snap *core.Snapshot
	err  error
}

// Model is the bubbletea state.
type Model struct {
	ctx     context.Context
	version string

	snap    *core.Snapshot
	loadErr error
	loading bool
	reload  int

	tab    tab
	cursor int
	help   bool

	dialog       dialogKind
	dialogTitle  string
	dialogLines  []string
	dialogOffset int
	dialogError  bool
	inputValue   string
	pendingPlan  *core.Plan
	notice       string

	width  int
	height int
}

// New creates the initial model. Detection runs asynchronously in Init.
func New(ctx context.Context, version string) Model {
	return Model{ctx: ctx, version: version, loading: true}
}

func (m Model) Init() tea.Cmd { return m.loadCmd() }

func (m Model) loadCmd() tea.Cmd {
	id, ctx := m.reload, m.ctx
	return func() tea.Msg {
		snap, err := detect.Scan(ctx)
		return loadedMsg{id: id, snap: snap, err: err}
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampDialogOffset()
		return m, nil

	case loadedMsg:
		if msg.id != m.reload {
			return m, nil
		}
		m.loading = false
		m.snap, m.loadErr = msg.snap, msg.err
		m.clampCursor()
		return m, nil

	case execDoneMsg:
		return m.handleExecDone(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.help {
		switch key {
		case "?", "esc", "q", "enter":
			m.help = false
			return m, nil
		}
		return m, nil
	}
	if m.dialog != dialogNone {
		return m.handleDialogKey(msg)
	}

	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "tab", "right", "l":
		m.tab = (m.tab + 1) % tabCount
		m.clampCursor()
	case "shift+tab", "left", "h":
		m.tab = (m.tab - 1 + tabCount) % tabCount
		m.clampCursor()
	case "1":
		m.tab = tabOverview
		m.clampCursor()
	case "2":
		m.tab = tabVersions
		m.clampCursor()
	case "3":
		m.tab = tabDoctor
		m.clampCursor()

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < m.cursorMax() {
			m.cursor++
		}
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = m.cursorMax()

	case "i":
		if m.tab == tabVersions {
			return m.openInstallInput()
		}
	case "s":
		if m.tab == tabVersions {
			return m.planSelected(core.ActionSwitch)
		}
	case "d":
		if m.tab == tabVersions {
			return m.planSelected(core.ActionRemove)
		}

	case "r":
		m.notice = ""
		m.reload++
		m.loading = true
		m.loadErr = nil
		return m, m.loadCmd()

	case "?":
		m.help = true
	}
	return m, nil
}

func (m Model) cursorMax() int {
	switch m.tab {
	case tabVersions:
		if n := len(m.versionItems()); n > 0 {
			return n - 1
		}
	case tabDoctor:
		if m.snap != nil && len(m.snap.Warnings) > 0 {
			return len(m.snap.Warnings) - 1
		}
	}
	return 0
}

func (m *Model) clampCursor() {
	maximum := m.cursorMax()
	if m.cursor > maximum {
		m.cursor = maximum
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// View implements tea.Model.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.dialog != dialogNone {
		return m.dialogView()
	}
	if m.help {
		return m.helpView()
	}

	var b strings.Builder
	header := m.headerView()
	tabs := m.tabBarView()
	body := m.bodyView()
	footer := m.footerView()

	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(tabs)
	b.WriteString("\n\n")

	reserved := lipgloss.Height(header) + lipgloss.Height(tabs) + lipgloss.Height(footer) + 2
	body = clipBody(body, m.height-reserved)
	b.WriteString(body)
	b.WriteString("\n")
	b.WriteString(footer)
	return b.String()
}

func (m Model) headerView() string {
	left := logoStyle.Render("⬢ phpman") + " " + mutedStyle.Render(m.version)
	right := ""
	if m.snap != nil {
		right = truncate(textStyle.Render(m.snap.Distro.PrettyName), max(12, m.width/2))
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	line := " " + left + strings.Repeat(" ", gap) + right
	return line + "\n" + " " + m.statusLine()
}

func (m Model) statusLine() string {
	if m.snap == nil {
		return mutedStyle.Render("detectando o ambiente PHP…")
	}
	s := m.snap
	var parts []string
	if a := s.Active(); a != nil {
		parts = append(parts, okStyle.Render("PHP "+a.Raw+" ativo"))
	} else if s.ActiveVersion != "" {
		parts = append(parts, okStyle.Render("PHP "+s.ActiveVersion+" ativo"))
	} else {
		parts = append(parts, warnStyle.Render("nenhum PHP ativo"))
	}

	activeFPM := 0
	for _, svc := range s.Services {
		if svc.Active {
			activeFPM++
		}
	}
	if len(s.Services) > 0 {
		state := fmt.Sprintf("FPM %d/%d ativo(s)", activeFPM, len(s.Services))
		if activeFPM == 0 {
			parts = append(parts, warnStyle.Render(state))
		} else {
			parts = append(parts, mutedStyle.Render(state))
		}
	}
	parts = append(parts, mutedStyle.Render(plural(len(s.Versions), "versão", "versões")))

	warns, _ := countWarnings(s.Warnings)
	if warns > 0 {
		parts = append(parts, warnStyle.Render(plural(warns, "aviso", "avisos")))
	} else {
		parts = append(parts, okStyle.Render("sem avisos"))
	}
	if m.loading {
		parts = append(parts, accentStyle.Render("atualizando…"))
	}
	if m.notice != "" {
		parts = append(parts, accentStyle.Render(m.notice))
	}
	return strings.Join(parts, mutedStyle.Render("  ·  "))
}

func (m Model) tabBarView() string {
	labels := []string{"Visão geral", "Versões", "Doutor"}
	parts := make([]string, 0, len(labels))
	for i, label := range labels {
		text := fmt.Sprintf("%d %s", i+1, label)
		if tab(i) == m.tab {
			parts = append(parts, activeTab.Render(text))
		} else {
			parts = append(parts, inactiveTab.Render(text))
		}
	}
	return " " + lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func (m Model) bodyView() string {
	if m.snap == nil {
		if m.loadErr != nil {
			return m.errorView()
		}
		return "\n  " + accentStyle.Render("Detectando instalações, SAPIs, serviços, sockets e sites…") +
			"\n\n  " + mutedStyle.Render("Nada é alterado nesta etapa. Nenhum acesso root é solicitado.")
	}
	switch m.tab {
	case tabVersions:
		return m.versionsView()
	case tabDoctor:
		return m.doctorView()
	default:
		return m.overviewView()
	}
}

func (m Model) errorView() string {
	body := errStyle.Bold(true).Render("Não foi possível detectar o ambiente.") + "\n\n" +
		textStyle.Render(m.loadErr.Error()) + "\n\n" +
		mutedStyle.Render("Pressione r para tentar novamente ou q para sair.")
	box := cardStyle.BorderForeground(colorErr).Width(max(30, m.contentWidth()-6)).Render(body)
	return "\n" + lipgloss.NewStyle().PaddingLeft(1).Render(box)
}

func (m Model) footerView() string {
	keys := []string{"tab/←→ abas", "↑↓ navegar", "r recarregar", "? ajuda", "q sair"}
	if m.tab == tabVersions && m.snap != nil {
		keys = []string{"i instalar", "s trocar", "d remover", "↑↓ navegar", "r recarregar", "? ajuda", "q sair"}
	}
	return " " + mutedStyle.Render(strings.Join(keys, "  ·  "))
}

func (m Model) helpView() string {
	rows := [][2]string{
		{"tab / ← →", "trocar de aba"},
		{"1 2 3", "ir direto para a aba"},
		{"↑ ↓ / k j", "navegar na lista"},
		{"g / G", "primeiro / último item"},
		{"i", "instalar versão (aba Versões)"},
		{"s", "trocar para a versão selecionada"},
		{"d", "remover a versão selecionada"},
		{"r", "redetectar o ambiente"},
		{"?", "fechar esta ajuda"},
		{"q / ctrl+c", "sair"},
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Atalhos") + "\n\n")
	for _, row := range rows {
		b.WriteString(accentStyle.Render(fmt.Sprintf("%-14s", row[0])) + mutedStyle.Render(row[1]) + "\n")
	}
	b.WriteString("\n" + mutedStyle.Render("Ações de escrita exibem o plano e usam o helper privilegiado."))
	box := cardStyle.Width(max(38, min(60, m.width-8))).Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) contentWidth() int {
	width := m.width - 2
	if width < 30 {
		width = 30
	}
	return width
}

func countWarnings(warnings []core.Warning) (warns, errs int) {
	for _, w := range warnings {
		switch w.Severity {
		case core.SeverityWarn:
			warns++
		case core.SeverityError:
			errs++
		}
	}
	return warns, errs
}

// clipBody truncates the body so the header and footer always stay visible.
func clipBody(s string, maxLines int) string {
	if maxLines <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}
	if maxLines == 1 {
		return lines[0]
	}
	lines = append(lines[:maxLines-1], mutedStyle.Render("  … conteúdo truncado — aumente a janela"))
	return strings.Join(lines, "\n")
}
