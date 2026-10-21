package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/lucasarakaki/phpman/internal/core"
)

type versionItem struct {
	raw       string
	installed bool
	active    bool
	inPath    bool
	version   *core.Version
	available *core.AvailableVersion
}

func (m Model) versionItems() []versionItem {
	if m.snap == nil {
		return nil
	}
	var items []versionItem
	installed := map[string]bool{}
	for i := range m.snap.Versions {
		v := &m.snap.Versions[i]
		installed[v.Raw] = true
		items = append(items, versionItem{
			raw: v.Raw, installed: true, active: v.Active, inPath: v.InPath, version: v,
		})
	}
	for i := range m.snap.Available {
		a := &m.snap.Available[i]
		if installed[a.Raw] {
			continue
		}
		items = append(items, versionItem{raw: a.Raw, available: a})
	}
	return items
}

type cardData struct {
	title string
	value string
	sub   string
}

func (m Model) overviewView() string {
	s := m.snap
	w := m.contentWidth()

	var b strings.Builder
	b.WriteString(renderCards(m.overviewCards(), w))
	b.WriteString("\n\n")
	b.WriteString(m.versionsSummary(w))
	if len(s.Warnings) > 0 {
		b.WriteString("\n\n")
		b.WriteString(m.alertsSummary(w))
	}
	if len(s.Vhosts) > 0 && m.height >= 28 {
		b.WriteString("\n\n")
		b.WriteString(m.vhostsSummary(w))
	}
	return b.String()
}

func (m Model) overviewCards() []cardData {
	s := m.snap
	activeValue, activeSub := "—", "nenhuma versão definida"
	if a := s.Active(); a != nil {
		activeValue = versionText(*a)
		activeSub = "padrão do sistema"
		if a.InPath {
			activeSub = "também no PATH"
		}
	} else if s.ActiveVersion != "" {
		activeValue = s.ActiveVersion
		activeSub = "padrão do sistema"
	}

	activeFPM, totalFPM := 0, 0
	for _, svc := range s.Services {
		totalFPM++
		if svc.Active {
			activeFPM++
		}
	}
	fpmValue, fpmSub := "não instalado", "nenhuma unidade FPM"
	if totalFPM > 0 {
		fpmValue = fmt.Sprintf("%d/%d %s", activeFPM, totalFPM, pluralWord(totalFPM, "ativo", "ativos"))
		fpmSub = plural(totalFPM, "unidade php-fpm", "unidades php-fpm")
		if activeFPM == 0 {
			fpmSub = "FPM parado"
		}
	}

	warns, errs := countWarnings(s.Warnings)
	diagValue, diagSub := "tudo certo", "nenhum problema"
	switch {
	case errs > 0:
		diagValue = plural(errs, "erro", "erros")
		diagSub = "veja a aba Doutor"
	case warns > 0:
		diagValue = plural(warns, "aviso", "avisos")
		diagSub = "veja a aba Doutor"
	}

	return []cardData{
		{
			title: "Distro",
			value: truncate(s.Distro.PrettyName, 26),
			sub:   strings.ToUpper(string(s.Distro.Family)) + " · " + s.Distro.PackageManager,
		},
		{title: "PHP ativo", value: activeValue, sub: activeSub},
		{title: "FPM", value: fpmValue, sub: fpmSub},
		{title: "Diagnóstico", value: diagValue, sub: diagSub},
	}
}

func renderCards(cards []cardData, width int) string {
	cols := 4
	switch {
	case width < 60:
		cols = 1
	case width < 110:
		cols = 2
	}
	if cols > len(cards) {
		cols = len(cards)
	}
	if cols < 1 {
		return ""
	}

	var rows []string
	for start := 0; start < len(cards); start += cols {
		end := min(start+cols, len(cards))
		cardWidth := (width - (end-start-1)*2) / (end - start)
		rendered := make([]string, 0, end-start)
		for _, c := range cards[start:end] {
			rendered = append(rendered, renderCard(c, cardWidth))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, interleave(rendered, "  ")...))
	}
	return strings.Join(rows, "\n")
}

func interleave(items []string, sep string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items)*2-1)
	for i, item := range items {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, item)
	}
	return out
}

func renderCard(c cardData, width int) string {
	inner := width - 4 // border (2) + horizontal padding (2)
	if inner < 8 {
		inner = 8
	}
	body := mutedStyle.Render(strings.ToUpper(c.title)) + "\n" +
		titleStyle.Render(truncate(c.value, inner)) + "\n" +
		mutedStyle.Render(truncate(c.sub, inner))
	return cardStyle.Width(inner + 2).Render(body)
}

func (m Model) versionsSummary(w int) string {
	items := m.versionItems()
	var b strings.Builder
	b.WriteString(sectionTitle("versões"))
	if len(items) == 0 {
		b.WriteString("\n  " + mutedStyle.Render("nenhuma instalação encontrada"))
		return b.String()
	}
	limit := min(len(items), 6)
	for i := 0; i < limit; i++ {
		b.WriteString("\n  " + m.versionLine(items[i]))
	}
	if len(items) > limit {
		b.WriteString("\n  " + mutedStyle.Render(fmt.Sprintf("… e mais %d — aba 2", len(items)-limit)))
	}
	return b.String()
}

func (m Model) versionLine(item versionItem) string {
	marker, state := "○", "instalada"
	switch {
	case item.active:
		marker, state = "●", "ativa"
	case item.inPath:
		marker, state = "●", "no PATH"
	case item.available != nil:
		marker, state = "+", "disponível"
	}

	markerStyled := textStyle.Render(marker)
	if item.active || item.inPath {
		markerStyled = okStyle.Render(marker)
	} else if item.available != nil {
		markerStyled = accentStyle.Render(marker)
	}

	return fmt.Sprintf("%s %s %s", markerStyled, titleStyle.Render(fmt.Sprintf("%-5s", item.raw)), mutedStyle.Render(state))
}

func (m Model) vhostsSummary(w int) string {
	s := m.snap
	var b strings.Builder
	b.WriteString(sectionTitle("sites que usam PHP"))
	limit := min(len(s.Vhosts), 4)
	for _, vh := range s.Vhosts[:limit] {
		name := vh.Server
		if name == "" {
			name = filepath.Base(vh.File) + ":" + strconv.Itoa(vh.Line)
		}
		target := vh.Target
		if target == "" {
			target = vh.Kind
		}
		b.WriteString("\n  " + textStyle.Render(truncate(name, 28)) + " " + mutedStyle.Render("→") + " " +
			mutedStyle.Render(truncate(target, max(12, w-34))))
	}
	if len(s.Vhosts) > limit {
		b.WriteString("\n  " + mutedStyle.Render(fmt.Sprintf("… e mais %d", len(s.Vhosts)-limit)))
	}
	return b.String()
}

func (m Model) alertsSummary(w int) string {
	var b strings.Builder
	b.WriteString(sectionTitle("avisos"))
	for i, warning := range m.snap.Warnings {
		if i >= 3 {
			break
		}
		b.WriteString("\n  " + severityIcon(warning.Severity) + " " +
			textStyle.Render(truncate(warning.Message, max(20, w-14))))
	}
	if len(m.snap.Warnings) > 3 {
		b.WriteString("\n  " + mutedStyle.Render("veja todos na aba Doutor"))
	}
	return b.String()
}

func sectionTitle(title string) string {
	return mutedStyle.Render(strings.ToUpper(title))
}

func (m Model) versionsView() string {
	items := m.versionItems()
	if len(items) == 0 {
		return "\n  " + mutedStyle.Render("Nenhuma versão de PHP encontrada nesta máquina.")
	}
	w := m.contentWidth()
	if w < 84 {
		return m.versionList(items, w) + "\n\n" + m.versionDetail(items, m.cursor, w)
	}
	listWidth := 30
	detailWidth := w - listWidth - 4
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.versionList(items, listWidth),
		"   ",
		m.versionDetail(items, m.cursor, detailWidth),
	)
}

func (m Model) versionList(items []versionItem, width int) string {
	var b strings.Builder
	b.WriteString(sectionTitle("instaladas e disponíveis"))
	b.WriteString("\n")
	inner := max(10, width-2)
	for i, item := range items {
		if i == m.cursor {
			b.WriteString(selectedItem.Width(inner).Render("▸ " + itemMarker(item) + " " +
				fmt.Sprintf("%-5s", item.raw) + " " + itemState(item)))
		} else {
			b.WriteString("  " + m.versionLine(item))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func itemMarker(item versionItem) string {
	switch {
	case item.active, item.inPath:
		return "●"
	case item.available != nil:
		return "+"
	default:
		return "○"
	}
}

func itemState(item versionItem) string {
	switch {
	case item.active:
		return "ativa"
	case item.inPath:
		return "no PATH"
	case item.available != nil:
		return "disponível"
	default:
		return "instalada"
	}
}

func (m Model) versionDetail(items []versionItem, index, width int) string {
	if len(items) == 0 {
		return ""
	}
	if index >= len(items) {
		index = len(items) - 1
	}
	item := items[index]

	if item.available != nil {
		a := item.available
		var b strings.Builder
		b.WriteString(titleStyle.Render("PHP "+a.Raw) + "  " + accentStyle.Render("disponível"))
		b.WriteString("\n" + divider(width) + "\n")
		b.WriteString(field("Situação", "não instalada", width) + "\n")
		b.WriteString(field("Pacote", a.Package, width) + "\n")
		b.WriteString(field("Candidato", a.Candidate, width) + "\n")
		b.WriteString(field("Origem", a.Origin, width) + "\n")
		b.WriteString(field("Repositório", a.Source, width) + "\n")
		b.WriteString("\n" + mutedStyle.Render(wrapWords("A instalação com plano e sudo será habilitada no M1.", width-4)))
		return b.String()
	}

	v := item.version
	var b strings.Builder
	header := titleStyle.Render("PHP " + versionText(*v))
	if v.Active {
		header += "  " + okStyle.Render("● ativa")
	}
	if v.InPath && !v.Active {
		header += "  " + okStyle.Render("● no PATH")
	}
	b.WriteString(header)
	b.WriteString("\n" + divider(width) + "\n")

	if v.Path != "" {
		b.WriteString(field("Binário CLI", v.Path, width) + "\n")
	}
	sapis := make([]string, 0, len(v.SAPIs))
	for _, sapi := range v.SAPIs {
		sapis = append(sapis, string(sapi.Kind))
	}
	b.WriteString(field("SAPIs", strings.Join(sapis, ", "), width) + "\n")

	if fpm, ok := v.SAPI(core.SAPIFPM); ok {
		state := fpm.State
		if state == "" {
			state = "desconhecido"
		}
		unit := fpm.Unit
		if unit == "" {
			unit = "sem unidade systemd"
		}
		b.WriteString(field("FPM", unit+" ("+state+")", width) + "\n")
		if fpm.Socket != "" {
			socketState := "presente"
			if !fpm.SocketExists {
				socketState = "ausente"
			}
			b.WriteString(field("Socket", fpm.Socket+" ("+socketState+")", width) + "\n")
		}
	} else {
		b.WriteString(field("FPM", "não instalado", width) + "\n")
	}

	if cli, ok := v.SAPI(core.SAPICLI); ok && cli.IniFile != "" {
		b.WriteString(field("php.ini", cli.IniFile, width) + "\n")
	}

	if len(v.Extensions) > 0 {
		names := make([]string, 0, len(v.Extensions))
		for _, ext := range v.Extensions {
			names = append(names, ext.Name)
		}
		sort.Strings(names)
		b.WriteString(field("Extensões", plural(len(names), "carregada", "carregadas"), width) + "\n")
		b.WriteString(wrapIndent(strings.Join(names, ", "), 12, width))
	}

	if len(v.Packages) > 0 {
		names := make([]string, 0, len(v.Packages))
		for _, pkg := range v.Packages {
			names = append(names, pkg.Name+" "+pkg.Version)
		}
		sort.Strings(names)
		b.WriteString("\n" + field("Pacotes", fmt.Sprintf("%d", len(names)), width) + "\n")
		b.WriteString(wrapIndent(strings.Join(names, ", "), 12, width))
	}
	return b.String()
}

func (m Model) doctorView() string {
	s := m.snap
	w := m.contentWidth()
	warns, errs := countWarnings(s.Warnings)

	if len(s.Warnings) == 0 {
		body := okStyle.Bold(true).Render("✓ Nenhum problema encontrado") + "\n\n" +
			mutedStyle.Render(fmt.Sprintf("%d versões, %d serviços, %d sockets, %d sites analisados.",
				len(s.Versions), len(s.Services), len(s.Sockets), len(s.Vhosts)))
		return "\n" + lipgloss.NewStyle().PaddingLeft(1).Render(cardStyle.Render(body))
	}

	var b strings.Builder
	b.WriteString(mutedStyle.Render(plural(warns, "aviso", "avisos") + " · " +
		plural(errs, "erro", "erros") + " · " + plural(len(s.Warnings), "verificação", "verificações")))
	b.WriteString("\n\n")
	for i, warning := range s.Warnings {
		b.WriteString(m.warningBlock(warning, w, i == m.cursor))
		if i < len(s.Warnings)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m Model) warningBlock(warning core.Warning, width int, selected bool) string {
	icon := severityIcon(warning.Severity)
	code := mutedStyle.Render(warning.Code)
	if warning.Severity == core.SeverityError {
		code = errStyle.Render(warning.Code)
	} else if warning.Severity == core.SeverityWarn {
		code = warnStyle.Render(warning.Code)
	}

	var b strings.Builder
	b.WriteString(icon + " " + code + "\n")
	b.WriteString("  " + textStyle.Render(wrapWords(warning.Message, max(20, width-6))))
	if warning.Hint != "" {
		b.WriteString("\n  " + accentStyle.Render("→ ") + mutedStyle.Render(wrapWords(warning.Hint, max(18, width-8))))
	}
	for _, detail := range warning.Details {
		b.WriteString("\n  " + mutedStyle.Render(truncate(detail, max(16, width-6))))
	}
	block := b.String()
	if selected {
		return selectedItem.Width(max(20, width-2)).Render(" " + block)
	}
	return " " + block
}

func severityIcon(s core.Severity) string {
	switch s {
	case core.SeverityError:
		return errStyle.Render("x")
	case core.SeverityWarn:
		return warnStyle.Render("!")
	default:
		return accentStyle.Render("i")
	}
}

func versionText(v core.Version) string {
	if v.Full != "" {
		return v.Full
	}
	return v.Raw
}

func divider(width int) string {
	return dividerStyle.Render(strings.Repeat("─", max(10, width)))
}

func field(label, value string, width int) string {
	const labelWidth = 12
	avail := max(10, width-labelWidth-2)
	lines := strings.Split(wrapWords(value, avail), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		lines = []string{""}
	}
	var b strings.Builder
	b.WriteString(labelStyle.Render(fmt.Sprintf("%-*s", labelWidth, label)))
	b.WriteString(textStyle.Render(lines[0]))
	for _, line := range lines[1:] {
		b.WriteString("\n" + strings.Repeat(" ", labelWidth) + textStyle.Render(line))
	}
	return b.String()
}

func wrapIndent(value string, indent, width int) string {
	wrapped := wrapWords(value, max(10, width-indent))
	prefix := strings.Repeat(" ", indent)
	return prefix + strings.ReplaceAll(wrapped, "\n", "\n"+prefix)
}

// wrapWords wraps text at word boundaries, hard-breaking words that exceed the
// available width. Widths are measured in runes to keep accents intact.
func wrapWords(text string, width int) string {
	if width <= 0 {
		return text
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	var lines []string
	current := ""
	for _, word := range words {
		chunks := hardChunks(word, width)
		for i, chunk := range chunks {
			last := i == len(chunks)-1
			switch {
			case last && current == "":
				current = chunk
			case last && runeLen(current)+1+runeLen(chunk) <= width:
				current += " " + chunk
			case last:
				lines = append(lines, current)
				current = chunk
			default:
				if current != "" {
					lines = append(lines, current)
					current = ""
				}
				lines = append(lines, chunk)
			}
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return strings.Join(lines, "\n")
}

func hardChunks(word string, width int) []string {
	runes := []rune(word)
	if width <= 0 || len(runes) <= width {
		return []string{word}
	}
	var chunks []string
	for len(runes) > width {
		chunks = append(chunks, string(runes[:width]))
		runes = runes[width:]
	}
	if len(runes) > 0 {
		chunks = append(chunks, string(runes))
	}
	return chunks
}

func runeLen(s string) int { return len([]rune(s)) }

func pluralWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func plural(n int, one, many string) string {
	return fmt.Sprintf("%d %s", n, pluralWord(n, one, many))
}

func truncate(s string, width int) string {
	if width <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width == 1 {
		return string(runes[:1])
	}
	return string(runes[:width-1]) + "…"
}
