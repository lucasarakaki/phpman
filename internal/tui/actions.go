package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lucasarakaki/phpman/internal/core"
	"github.com/lucasarakaki/phpman/internal/provider"
	"github.com/lucasarakaki/phpman/internal/provider/registry"
	"github.com/lucasarakaki/phpman/internal/report"
)

type dialogKind int

const (
	dialogNone dialogKind = iota
	dialogInput
	dialogPlan
	dialogMessage
)

type execDoneMsg struct {
	err error
}

func (m Model) selectedVersionItem() (versionItem, bool) {
	items := m.versionItems()
	if m.cursor < 0 || m.cursor >= len(items) {
		return versionItem{}, false
	}
	return items[m.cursor], true
}

func (m Model) openInstallInput() (Model, tea.Cmd) {
	if m.snap == nil {
		return m.withMessage("Ambiente indisponível", "A detecção ainda não terminou. Aguarde e tente novamente.", true), nil
	}
	value := ""
	if item, ok := m.selectedVersionItem(); ok {
		value = item.raw
	}
	m.notice = ""
	m.dialog = dialogInput
	m.dialogTitle = "Instalar versão"
	m.dialogLines = nil
	m.dialogOffset = 0
	m.dialogError = false
	m.inputValue = value
	m.pendingPlan = nil
	return m, nil
}

func (m Model) planSelected(action core.Action) (Model, tea.Cmd) {
	if m.snap == nil {
		return m.withMessage("Ambiente indisponível", "A detecção ainda não terminou. Aguarde e tente novamente.", true), nil
	}
	item, ok := m.selectedVersionItem()
	if !ok {
		return m.withMessage("Nenhuma versão selecionada", "Use ↑/↓ para escolher uma versão na aba Versões.", true), nil
	}
	if item.version == nil {
		return m.withMessage(
			"Versão não instalada",
			"A versão "+item.raw+" ainda não está instalada. Use i para planejar a instalação.",
			true,
		), nil
	}
	return m.planAction(action, item.raw)
}

func (m Model) planAction(action core.Action, raw string) (Model, tea.Cmd) {
	if m.snap == nil {
		return m.withMessage("Ambiente indisponível", "A detecção ainda não terminou. Aguarde e tente novamente.", true), nil
	}
	prov, err := registry.For(m.snap.Distro)
	if err != nil {
		return m.withError("Ação indisponível", err), nil
	}

	var plan *core.Plan
	switch action {
	case core.ActionInstall:
		opts := provider.DefaultOptions()
		plan, err = prov.PlanInstall(m.snap, raw, opts)
		if err != nil && errors.Is(err, provider.ErrVersionUnavailable) {
			// A geração do plano é somente leitura. O repositório só é
			// adicionado depois da confirmação no plano e no CLI.
			opts.AllowThirdParty = true
			plan, err = prov.PlanInstall(m.snap, raw, opts)
		}
	case core.ActionRemove:
		plan, err = prov.PlanRemove(m.snap, raw)
	case core.ActionSwitch:
		plan, err = prov.PlanSwitch(m.snap, raw)
	default:
		err = fmt.Errorf("ação desconhecida: %s", action)
	}
	if err != nil {
		return m.withError("Não foi possível gerar o plano", err), nil
	}
	return m.withPlan(plan), nil
}

func (m Model) withPlan(plan *core.Plan) Model {
	if plan == nil {
		return m.withMessage("Plano ausente", "O provider não retornou um plano.", true)
	}
	if len(plan.Steps) == 0 {
		text := plan.Summary
		if len(plan.Warnings) > 0 {
			text += "\n\n" + strings.Join(plan.Warnings, "\n")
		}
		return m.withMessage("Nada a executar", text, false)
	}

	var b strings.Builder
	report.WritePlanExecution(&b, plan, false)
	title := "Revisar plano"
	if plan.Repository != nil {
		title = "Revisar plano (terceiros)"
	}
	m.dialog = dialogPlan
	m.dialogTitle = title
	m.dialogLines = splitLines(b.String())
	m.dialogOffset = 0
	m.dialogError = false
	m.inputValue = ""
	m.pendingPlan = plan
	m.notice = ""
	return m
}

func (m Model) withError(title string, err error) Model {
	text := ""
	if err != nil {
		text = err.Error()
	}
	return m.withMessage(title, text, true)
}

func (m Model) withMessage(title, text string, isError bool) Model {
	m.dialog = dialogMessage
	m.dialogTitle = title
	m.dialogLines = wrapDialogText(text, m.dialogTextWidth())
	m.dialogOffset = 0
	m.dialogError = isError
	m.inputValue = ""
	m.pendingPlan = nil
	m.notice = ""
	return m
}

func (m Model) closeDialog() Model {
	m.dialog = dialogNone
	m.dialogTitle = ""
	m.dialogLines = nil
	m.dialogOffset = 0
	m.dialogError = false
	m.inputValue = ""
	m.pendingPlan = nil
	return m
}

func (m Model) handleDialogKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.dialog {
	case dialogInput:
		switch key {
		case "esc":
			return m.closeDialog(), nil
		case "enter":
			raw := strings.TrimSpace(m.inputValue)
			if raw == "" {
				return m, nil
			}
			m = m.closeDialog()
			return m.planAction(core.ActionInstall, raw)
		case "backspace", "ctrl+h":
			runes := []rune(m.inputValue)
			if len(runes) > 0 {
				m.inputValue = string(runes[:len(runes)-1])
			}
			return m, nil
		case "ctrl+u":
			m.inputValue = ""
			return m, nil
		}
		runes := []rune(m.inputValue)
		for _, r := range msg.Runes {
			if (r >= '0' && r <= '9' || r == '.') && len(runes) < 8 {
				runes = append(runes, r)
			}
		}
		m.inputValue = string(runes)
		return m, nil

	case dialogPlan:
		switch key {
		case "esc", "n", "q":
			return m.closeDialog(), nil
		case "enter":
			return m.startExecution()
		case "up", "k":
			return m.scrollDialog(-1), nil
		case "down", "j":
			return m.scrollDialog(1), nil
		case "pgup", "ctrl+b":
			return m.scrollDialog(-m.dialogVisibleLines()), nil
		case "pgdown", "ctrl+f":
			return m.scrollDialog(m.dialogVisibleLines()), nil
		case "home", "g":
			m.dialogOffset = 0
			return m, nil
		case "end", "G":
			m.dialogOffset = max(0, len(m.dialogLines)-m.dialogVisibleLines())
			return m, nil
		}
		return m, nil

	case dialogMessage:
		switch key {
		case "esc", "enter", "n", "q":
			return m.closeDialog(), nil
		case "up", "k":
			return m.scrollDialog(-1), nil
		case "down", "j":
			return m.scrollDialog(1), nil
		case "pgup", "ctrl+b":
			return m.scrollDialog(-m.dialogVisibleLines()), nil
		case "pgdown", "ctrl+f":
			return m.scrollDialog(m.dialogVisibleLines()), nil
		case "home", "g":
			m.dialogOffset = 0
			return m, nil
		case "end", "G":
			m.dialogOffset = max(0, len(m.dialogLines)-m.dialogVisibleLines())
			return m, nil
		}
		return m, nil
	}
	return m, nil
}

func (m Model) startExecution() (Model, tea.Cmd) {
	if m.pendingPlan == nil {
		return m.withError("Plano ausente", errors.New("nenhum plano selecionado")), nil
	}
	cmd, err := buildExecCommand(m.pendingPlan)
	if err != nil {
		return m.withError("Não foi possível iniciar a execução", err), nil
	}
	m = m.closeDialog()
	m.notice = ""
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return execDoneMsg{err: err}
	})
}

func buildExecCommand(plan *core.Plan) (*exec.Cmd, error) {
	if plan == nil {
		return nil, errors.New("plano ausente")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("não foi possível localizar o executável: %w", err)
	}
	args := []string{string(plan.Action), plan.Version}
	if plan.Action == core.ActionInstall && plan.Repository != nil {
		args = append(args, "--third-party")
	}
	return exec.Command(exe, args...), nil
}

func (m Model) handleExecDone(msg execDoneMsg) (Model, tea.Cmd) {
	m.reload++
	m.loading = true
	m.loadErr = nil
	cmd := m.loadCmd()
	if msg.err != nil {
		text := msg.err.Error() + "\n\nA saída completa fica no terminal (visível ao sair da TUI); consulte também /var/lib/phpman/journal.jsonl."
		return m.withMessage("Execução falhou", text, true), cmd
	}
	m.notice = "ação encerrada; snapshot atualizado"
	return m, cmd
}

func (m Model) scrollDialog(delta int) Model {
	m.dialogOffset += delta
	m.clampDialogOffset()
	return m
}

func (m *Model) clampDialogOffset() {
	maximum := len(m.dialogLines) - m.dialogVisibleLines()
	if maximum < 0 {
		maximum = 0
	}
	if m.dialogOffset > maximum {
		m.dialogOffset = maximum
	}
	if m.dialogOffset < 0 {
		m.dialogOffset = 0
	}
}

func (m Model) dialogVisibleLines() int {
	width := m.dialogTextWidth()
	titleLines := len(strings.Split(wrapWords(m.dialogTitle, width), "\n"))
	footerLines := len(strings.Split(wrapWords(m.dialogFooter(), width), "\n"))
	visible := m.height - titleLines - footerLines - 8
	if visible < 3 {
		visible = 3
	}
	return visible
}

func (m Model) dialogCardWidth() int {
	width := m.width - 6
	if width < 12 {
		width = 12
	}
	return width
}

func (m Model) dialogTextWidth() int {
	width := m.dialogCardWidth() - 2
	if width < 8 {
		width = 8
	}
	return width
}

func (m Model) dialogView() string {
	if m.width == 0 {
		return ""
	}
	width := m.dialogCardWidth()
	contentWidth := m.dialogTextWidth()

	var b strings.Builder
	title := titleStyle.Render(m.dialogTitle)
	if m.dialogError {
		title = errStyle.Bold(true).Render(m.dialogTitle)
	}
	b.WriteString(title)
	b.WriteString("\n\n")
	if m.dialog == dialogInput {
		b.WriteString(m.inputDialogBody(contentWidth))
	} else {
		b.WriteString(m.linesDialogBody(contentWidth))
	}
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render(wrapWords(m.dialogFooter(), contentWidth)))

	box := cardStyle.Width(width).Render(b.String())
	return lipgloss.NewStyle().Padding(1, 2).Render(box)
}

func (m Model) inputDialogBody(width int) string {
	var b strings.Builder
	b.WriteString(textStyle.Render("Versão no formato X.Y:"))
	b.WriteString("\n\n  ")
	b.WriteString(accentStyle.Render("> "))
	if m.inputValue == "" {
		b.WriteString(mutedStyle.Render("8.3"))
	} else {
		b.WriteString(textStyle.Render(m.inputValue))
	}
	b.WriteString(accentStyle.Render("▌"))
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render(wrapWords(
		"Ex.: 8.3, 8.4. Se a versão não estiver no catálogo, o plano mostrará o repositório Sury/ondrej antes de executar.",
		width,
	)))
	return b.String()
}

func (m Model) linesDialogBody(width int) string {
	if len(m.dialogLines) == 0 {
		return mutedStyle.Render("sem detalhes")
	}
	visible := m.dialogVisibleLines()
	start := m.dialogOffset
	if start > len(m.dialogLines) {
		start = len(m.dialogLines)
	}
	end := min(start+visible, len(m.dialogLines))

	var b strings.Builder
	for _, line := range m.dialogLines[start:end] {
		b.WriteString(truncate(line, width))
		b.WriteString("\n")
	}
	if len(m.dialogLines) > visible {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("linhas %d-%d de %d", start+1, end, len(m.dialogLines))))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) dialogFooter() string {
	switch m.dialog {
	case dialogInput:
		return "enter continuar · esc cancelar"
	case dialogPlan:
		return "↑↓ rolar · enter continuar · esc cancelar — o terminal pedirá a confirmação final"
	default:
		return "↑↓ rolar · enter/esc fechar"
	}
}

func splitLines(text string) []string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func wrapDialogText(text string, width int) []string {
	if width < 10 {
		width = 10
	}
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, strings.Split(wrapWords(line, width), "\n")...)
	}
	return lines
}
