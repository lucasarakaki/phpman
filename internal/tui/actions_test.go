package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lucasarakaki/phpman/internal/core"
)

func TestInstallInputDefaultsToSelectedAvailable(t *testing.T) {
	m := fixtureModel(100, 30)
	m.tab = tabVersions
	m.cursor = 1

	updated, _ := m.openInstallInput()
	if updated.dialog != dialogInput {
		t.Fatalf("dialog = %d, want dialogInput", updated.dialog)
	}
	if updated.inputValue != "8.3" {
		t.Errorf("inputValue = %q, want 8.3", updated.inputValue)
	}
}

func TestInstallPlanDialog(t *testing.T) {
	m := fixtureModel(120, 40)

	updated, _ := m.planAction(core.ActionInstall, "8.3")
	if updated.dialog != dialogPlan {
		t.Fatalf("dialog = %d, want dialogPlan", updated.dialog)
	}
	if updated.pendingPlan == nil {
		t.Fatal("pendingPlan ausente")
	}
	text := strings.Join(updated.dialogLines, "\n")
	if !strings.Contains(text, "Instalar PHP 8.3") {
		t.Errorf("plano sem resumo esperado:\n%s", text)
	}
}

func TestInstallUnavailableUsesThirdPartyPlan(t *testing.T) {
	m := fixtureModel(120, 40)

	updated, _ := m.planAction(core.ActionInstall, "8.4")
	if updated.dialog != dialogPlan {
		t.Fatalf("dialog = %d, want dialogPlan", updated.dialog)
	}
	if updated.pendingPlan == nil || updated.pendingPlan.Repository == nil {
		t.Fatal("plano de terceiros sem metadados de repositório")
	}
	if got := updated.pendingPlan.Repository.Fingerprint; got != "15058500A0235D97F5D10063B188E2B695BD4743" {
		t.Errorf("fingerprint = %q", got)
	}
	text := strings.Join(updated.dialogLines, "\n")
	if !strings.Contains(text, "REPOSITÓRIO DE TERCEIROS") {
		t.Errorf("plano sem seção de repositório:\n%s", text)
	}
	if !strings.Contains(text, "15058500A0235D97F5D10063B188E2B695BD4743") {
		t.Errorf("plano sem fingerprint:\n%s", text)
	}
}

func TestRemoveActiveVersionShowsError(t *testing.T) {
	m := fixtureModel(100, 30)

	updated, _ := m.planAction(core.ActionRemove, "8.2")
	if updated.dialog != dialogMessage || !updated.dialogError {
		t.Fatalf("dialog = %d, erro = %v; want mensagem de erro", updated.dialog, updated.dialogError)
	}
	if text := strings.Join(updated.dialogLines, "\n"); !strings.Contains(text, "versão ativa") {
		t.Errorf("erro inesperado:\n%s", text)
	}
}

func TestSwitchActiveVersionShowsNoop(t *testing.T) {
	m := fixtureModel(100, 30)

	updated, _ := m.planAction(core.ActionSwitch, "8.2")
	if updated.dialog != dialogMessage || updated.dialogError {
		t.Fatalf("dialog = %d, erro = %v; want mensagem informativa", updated.dialog, updated.dialogError)
	}
	if text := strings.Join(updated.dialogLines, "\n"); !strings.Contains(text, "Nenhuma alteração necessária") {
		t.Errorf("mensagem inesperada:\n%s", text)
	}
}

func TestPlanSelectedOnAvailableRequiresInstall(t *testing.T) {
	m := fixtureModel(100, 30)
	m.tab = tabVersions
	m.cursor = 1

	updated, _ := m.planSelected(core.ActionSwitch)
	if updated.dialog != dialogMessage || !updated.dialogError {
		t.Fatalf("dialog = %d, erro = %v; want mensagem de erro", updated.dialog, updated.dialogError)
	}
	if text := strings.Join(updated.dialogLines, "\n"); !strings.Contains(text, "Use i") {
		t.Errorf("mensagem inesperada:\n%s", text)
	}
}

func TestInputAcceptsOnlyVersionCharacters(t *testing.T) {
	m := fixtureModel(100, 30)
	m.dialog = dialogInput

	for _, r := range []rune{'8', '.', '4', 'x', ' '} {
		updated, _ := m.handleDialogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated
	}
	if m.inputValue != "8.4" {
		t.Errorf("inputValue = %q, want 8.4", m.inputValue)
	}

	updated, _ := m.handleDialogKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if updated.inputValue != "8." {
		t.Errorf("backspace = %q, want 8.", updated.inputValue)
	}
}

func TestInputEnterPlansInstall(t *testing.T) {
	m := fixtureModel(120, 40)
	m.dialog = dialogInput
	m.inputValue = "8.3"

	updated, _ := m.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.dialog != dialogPlan {
		t.Fatalf("dialog = %d, want dialogPlan", updated.dialog)
	}
	if updated.pendingPlan == nil || updated.pendingPlan.Version != "8.3" {
		t.Fatalf("plano inesperado: %+v", updated.pendingPlan)
	}
}

func TestInstallKeyOpensInput(t *testing.T) {
	m := fixtureModel(100, 30)
	m.tab = tabVersions
	m.cursor = 1

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	got := updated.(Model)
	if got.dialog != dialogInput {
		t.Fatalf("dialog = %d, want dialogInput", got.dialog)
	}
	if got.inputValue != "8.3" {
		t.Errorf("inputValue = %q, want 8.3", got.inputValue)
	}
}

func TestSwitchKeyOnActiveVersionShowsNoop(t *testing.T) {
	m := fixtureModel(100, 30)
	m.tab = tabVersions
	m.cursor = 0

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	got := updated.(Model)
	if got.dialog != dialogMessage || got.dialogError {
		t.Fatalf("dialog = %d, erro = %v; want mensagem informativa", got.dialog, got.dialogError)
	}
}

func TestPlanDialogEnterStartsExecution(t *testing.T) {
	m := fixtureModel(120, 40)
	updated, _ := m.planAction(core.ActionInstall, "8.3")

	updated, cmd := updated.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.dialog != dialogNone {
		t.Fatalf("dialog = %d, want dialogNone", updated.dialog)
	}
	if cmd == nil {
		t.Fatal("enter deveria iniciar a execução")
	}
}

func TestBuildExecCommand(t *testing.T) {
	install := &core.Plan{
		Action:     core.ActionInstall,
		Version:    "8.4",
		Repository: &core.Repository{Name: "Sury/ondrej"},
	}
	cmd, err := buildExecCommand(install)
	if err != nil {
		t.Fatalf("buildExecCommand: %v", err)
	}
	if want := []string{"install", "8.4", "--third-party"}; !reflect.DeepEqual(cmd.Args[1:], want) {
		t.Errorf("args = %#v, want %#v", cmd.Args[1:], want)
	}

	remove := &core.Plan{Action: core.ActionRemove, Version: "8.2"}
	cmd, err = buildExecCommand(remove)
	if err != nil {
		t.Fatalf("buildExecCommand remove: %v", err)
	}
	if want := []string{"remove", "8.2"}; !reflect.DeepEqual(cmd.Args[1:], want) {
		t.Errorf("args = %#v, want %#v", cmd.Args[1:], want)
	}

	if _, err := buildExecCommand(nil); err == nil {
		t.Fatal("plano ausente deveria falhar")
	}
}

func TestExecDoneReloadsAndReportsError(t *testing.T) {
	m := fixtureModel(100, 30)

	updated, cmd := m.handleExecDone(execDoneMsg{})
	if !updated.loading {
		t.Error("loading deveria ser true")
	}
	if updated.reload != m.reload+1 {
		t.Errorf("reload = %d, want %d", updated.reload, m.reload+1)
	}
	if updated.notice == "" {
		t.Error("notice de conclusão ausente")
	}
	if cmd == nil {
		t.Error("reload deveria retornar comando")
	}

	updated, cmd = m.handleExecDone(execDoneMsg{err: errors.New("falhou")})
	if updated.dialog != dialogMessage || !updated.dialogError {
		t.Fatalf("dialog = %d, erro = %v; want erro", updated.dialog, updated.dialogError)
	}
	if text := strings.Join(updated.dialogLines, "\n"); !strings.Contains(text, "falhou") {
		t.Errorf("erro inesperado:\n%s", text)
	}
	if cmd == nil {
		t.Error("reload após erro deveria retornar comando")
	}
}

func TestDialogScrollClamps(t *testing.T) {
	m := fixtureModel(100, 24)
	m.dialog = dialogPlan
	m.dialogLines = make([]string, 100)

	updated := m.scrollDialog(1000)
	want := 100 - m.dialogVisibleLines()
	if updated.dialogOffset != want {
		t.Errorf("offset = %d, want %d", updated.dialogOffset, want)
	}
	updated = updated.scrollDialog(-1000)
	if updated.dialogOffset != 0 {
		t.Errorf("offset = %d, want 0", updated.dialogOffset)
	}
}

func TestDialogView(t *testing.T) {
	m := fixtureModel(100, 30)
	m = m.withMessage("Teste", strings.Repeat("linha longa ", 20), true)

	out := m.View()
	if !strings.Contains(out, "Teste") {
		t.Errorf("título ausente:\n%s", out)
	}
	if !strings.Contains(out, "enter/esc") {
		t.Errorf("rodapé ausente:\n%s", out)
	}
}

func TestPlanDialogView(t *testing.T) {
	m := fixtureModel(80, 35)
	updated, _ := m.planAction(core.ActionInstall, "8.3")

	out := updated.View()
	if !strings.Contains(out, "Revisar plano") {
		t.Errorf("título ausente:\n%s", out)
	}
	if !strings.Contains(out, "Instalar PHP 8.3") {
		t.Errorf("resumo ausente:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if lipgloss.Width(line) > updated.width {
			t.Errorf("linha mais larga que o terminal (%d > %d): %q", lipgloss.Width(line), updated.width, line)
		}
	}
}

func TestThirdPartyPlanDialogView(t *testing.T) {
	m := fixtureModel(80, 35)
	updated, _ := m.planAction(core.ActionInstall, "8.4")

	out := updated.View()
	if !strings.Contains(out, "Revisar plano (terceiros)") {
		t.Errorf("título de terceiros ausente:\n%s", out)
	}
	if !strings.Contains(out, "15058500A0235D97F5D10063B188E2B695BD4743") {
		t.Errorf("fingerprint ausente:\n%s", out)
	}
}

func TestNarrowDialogDoesNotPanic(t *testing.T) {
	m := fixtureModel(40, 20)
	m = m.withMessage("Aviso", strings.Repeat("texto ", 30), false)
	out := m.View()
	if strings.TrimSpace(out) == "" {
		t.Fatal("diálogo vazio em terminal estreito")
	}
	for _, line := range strings.Split(out, "\n") {
		if lipgloss.Width(line) > m.width {
			t.Errorf("linha mais larga que o terminal (%d > %d): %q", lipgloss.Width(line), m.width, line)
		}
	}
}

func TestNarrowThirdPartyPlanFitsWidth(t *testing.T) {
	m := fixtureModel(40, 24)
	updated, _ := m.planAction(core.ActionInstall, "8.4")

	out := updated.View()
	for _, line := range strings.Split(out, "\n") {
		if lipgloss.Width(line) > updated.width {
			t.Errorf("linha mais larga que o terminal (%d > %d): %q", lipgloss.Width(line), updated.width, line)
		}
	}
}
