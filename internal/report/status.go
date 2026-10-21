// Package report renders snapshots as human readable text or JSON.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/lucasarakaki/phpman/internal/core"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
)

// Painter applies ANSI styling when enabled.
type Painter struct{ enabled bool }

// NewPainter respects NO_COLOR and the caller's TTY detection.
func NewPainter(enabled bool) Painter {
	if os.Getenv("NO_COLOR") != "" {
		enabled = false
	}
	return Painter{enabled: enabled}
}

func (p Painter) style(code, s string) string {
	if !p.enabled || s == "" {
		return s
	}
	return code + s + ansiReset
}

func (p Painter) Bold(s string) string   { return p.style(ansiBold, s) }
func (p Painter) Dim(s string) string    { return p.style(ansiDim, s) }
func (p Painter) Red(s string) string    { return p.style(ansiRed, s) }
func (p Painter) Green(s string) string  { return p.style(ansiGreen, s) }
func (p Painter) Yellow(s string) string { return p.style(ansiYellow, s) }
func (p Painter) Cyan(s string) string   { return p.style(ansiCyan, s) }

// WriteJSON encodes the snapshot as indented JSON.
func WriteJSON(w io.Writer, s *core.Snapshot) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(s)
}

// WriteText renders the full status report.
func WriteText(w io.Writer, s *core.Snapshot, version string, color bool) {
	p := NewPainter(color)

	fmt.Fprintf(w, "%s %s  %s\n", p.Bold("phpman"), p.Dim(version), p.Dim("· leitura somente"))
	fmt.Fprintf(w, "%s\n", p.Bold(s.Distro.PrettyName))
	fmt.Fprintf(w, "%s\n", p.Dim(fmt.Sprintf("família %s · %s · %s",
		string(s.Distro.Family), packageManager(s.Distro), s.GeneratedAt.Format("2006-01-02 15:04:05"))))

	writeUsage(w, p, s)
	writeVersions(w, p, s)
	writeAvailable(w, p, s)
	writeSockets(w, p, s)
	writeVhosts(w, p, s)
	writeExtensions(w, p, s)
	writeWarnings(w, p, s)
	writeCapabilities(w, p, s)
	writeNotes(w, p, s)
}

func packageManager(d core.Distro) string {
	if d.PackageManager == "" {
		return "gerenciador desconhecido"
	}
	return d.PackageManager
}

func section(w io.Writer, p Painter, title string) {
	fmt.Fprintf(w, "\n%s\n", p.Dim(strings.ToUpper(title)))
}

func writeUsage(w io.Writer, p Painter, s *core.Snapshot) {
	section(w, p, "PHP em uso")

	system, systemSource := s.ActiveVersion, "update-alternatives"
	if system == "" {
		system, systemSource = "—", "nenhum padrão definido"
	} else if len(s.Alternatives) == 0 {
		systemSource = "definido pelo PATH"
	}
	pathValue := s.PathVersion
	if pathValue == "" {
		pathValue = "—"
	}

	fmt.Fprintf(w, "  %-9s %-6s %s\n", "sistema", p.Bold(system), p.Dim("("+systemSource+")"))
	fmt.Fprintf(w, "  %-9s %-6s %s\n", "PATH", p.Bold(pathValue), p.Dim(shortPath(s.PathBinary)))

	for _, svc := range s.Services {
		state := svc.State
		mark := p.Yellow("○")
		if svc.Active {
			mark = p.Green("●")
			state = "ativa"
		}
		version := svc.Version
		if version == "" {
			version = "—"
		}
		fmt.Fprintf(w, "  %s %-24s %-5s %s\n", mark, svc.Unit, version, p.Dim(state))
	}
}

func writeVersions(w io.Writer, p Painter, s *core.Snapshot) {
	section(w, p, "versões instaladas")
	if len(s.Versions) == 0 {
		fmt.Fprintf(w, "  %s\n", p.Dim("nenhuma"))
		return
	}
	for _, v := range s.Versions {
		mark := p.Dim("○")
		state := "instalada"
		if v.Active {
			mark = p.Green("●")
			state = "ativa"
		}
		if v.InPath && !v.Active {
			state = "no PATH"
		}
		sapis := make([]string, 0, len(v.SAPIs))
		for _, sp := range v.SAPIs {
			sapis = append(sapis, string(sp.Kind))
		}
		version := v.Raw
		if v.Full != "" && v.Full != v.Raw {
			version = v.Full
		}
		fmt.Fprintf(w, "  %s %s %s %-18s %s\n",
			mark,
			p.Bold(fmt.Sprintf("%-8s", version)),
			p.Dim(fmt.Sprintf("%-8s", state)),
			strings.Join(sapis, ", "),
			p.Dim(v.Path))

		if fpm, ok := v.SAPI(core.SAPIFPM); ok {
			unit := fpm.Unit
			if unit == "" {
				unit = "sem unidade systemd"
			}
			detail := fmt.Sprintf("%s · %s", unit, fpm.State)
			if fpm.Socket != "" {
				detail += "  socket " + fpm.Socket
				if !fpm.SocketExists {
					detail += " (ausente)"
				}
			}
			fmt.Fprintf(w, "%s%s\n", strings.Repeat(" ", 10), p.Dim(detail))
		}
	}
}

func writeAvailable(w io.Writer, p Painter, s *core.Snapshot) {
	if len(s.Available) == 0 {
		return
	}
	section(w, p, "disponíveis para instalação")
	for _, av := range s.Available {
		origin := "repo da distro"
		if av.Origin == "terceiros" {
			origin = p.Yellow("terceiros")
		}
		installed := ""
		if av.Installed {
			installed = p.Green(" instalada")
		}
		source := av.Source
		if source != "" {
			source = "  " + p.Dim(source)
		}
		fmt.Fprintf(w, "  %-6s %-16s %-24s%s%s\n", p.Bold(av.Raw), origin, av.Candidate, source, installed)
	}
}

func writeSockets(w io.Writer, p Painter, s *core.Snapshot) {
	if len(s.Sockets) == 0 {
		return
	}
	section(w, p, "sockets do FPM")
	for _, sock := range s.Sockets {
		mark := p.Green("✓")
		if !sock.Exists {
			mark = p.Yellow("✗")
		}
		version := sock.Version
		if version == "" {
			version = "—"
		}
		fmt.Fprintf(w, "  %s %-42s %-5s %s\n", mark, sock.Path, version, p.Dim(sock.Source))
	}
}

func writeVhosts(w io.Writer, p Painter, s *core.Snapshot) {
	if len(s.Vhosts) == 0 {
		return
	}
	section(w, p, "sites que usam PHP")
	limit := len(s.Vhosts)
	if limit > 12 {
		limit = 12
	}
	for _, vh := range s.Vhosts[:limit] {
		name := vh.Server
		if name == "" {
			name = "sem server_name"
		}
		target := vh.Target
		if target == "" {
			target = vh.Kind
		}
		fmt.Fprintf(w, "  %s  %-28s → %s\n", p.Dim(fmt.Sprintf("%s:%d", vh.File, vh.Line)), name, target)
	}
	if len(s.Vhosts) > limit {
		fmt.Fprintf(w, "  %s\n", p.Dim(fmt.Sprintf("… e mais %d", len(s.Vhosts)-limit)))
	}
}

func writeExtensions(w io.Writer, p Painter, s *core.Snapshot) {
	active := s.Active()
	if active == nil || len(active.Extensions) == 0 {
		return
	}
	section(w, p, fmt.Sprintf("extensões carregadas (PHP %s)", active.Raw))
	names := make([]string, 0, len(active.Extensions))
	for _, e := range active.Extensions {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	for _, line := range wrapString(strings.Join(names, ", "), 96) {
		fmt.Fprintf(w, "  %s\n", line)
	}
	fmt.Fprintf(w, "  %s\n", p.Dim(plural(len(names), "extensão", "extensões")))
}

func writeWarnings(w io.Writer, p Painter, s *core.Snapshot) {
	section(w, p, "diagnóstico")
	if len(s.Warnings) == 0 {
		fmt.Fprintf(w, "  %s nenhum problema encontrado\n", p.Green("✓"))
		return
	}
	warns, errs := Counts(s.Warnings)
	fmt.Fprintf(w, "  %s\n", p.Dim(plural(warns, "aviso", "avisos")+", "+plural(errs, "erro", "erros")))
	writeWarningList(w, p, s.Warnings)
}

func writeWarningList(w io.Writer, p Painter, warnings []core.Warning) {
	for _, warning := range warnings {
		icon, paint := "i", p.Cyan
		switch warning.Severity {
		case core.SeverityWarn:
			icon, paint = "!", p.Yellow
		case core.SeverityError:
			icon, paint = "x", p.Red
		}
		fmt.Fprintf(w, "  %s %s  %s\n", paint(icon), p.Bold(warning.Code), warning.Message)
		if warning.Hint != "" {
			fmt.Fprintf(w, "      %s %s\n", p.Dim("→"), warning.Hint)
		}
		for _, detail := range warning.Details {
			fmt.Fprintf(w, "      %s\n", p.Dim(detail))
		}
	}
}

func writeCapabilities(w io.Writer, p Painter, s *core.Snapshot) {
	section(w, p, "capacidades")
	for _, c := range s.Capabilities {
		mark := p.Dim("·")
		label := c.Label
		if c.Supported {
			mark = p.Green("✓")
			label = p.Bold(label)
		}
		phase := c.Phase
		if phase != "" {
			phase = " " + p.Dim("("+phase+")")
		}
		fmt.Fprintf(w, "  %s %s %s%s\n", mark, label, p.Dim("— "+c.Detail), phase)
	}
}

func writeNotes(w io.Writer, p Painter, s *core.Snapshot) {
	if len(s.Notes) == 0 {
		return
	}
	section(w, p, "notas")
	for _, note := range s.Notes {
		fmt.Fprintf(w, "  %s %s\n", p.Dim("·"), p.Dim(note))
	}
}

// WriteDoctor renders only diagnostics and capabilities.
func WriteDoctor(w io.Writer, s *core.Snapshot, color bool) {
	p := NewPainter(color)
	warns, errs := Counts(s.Warnings)

	fmt.Fprintf(w, "%s %s\n", p.Bold("phpman doctor"), p.Dim("· "+s.Distro.PrettyName))
	fmt.Fprintf(w, "%s\n", p.Dim(plural(len(s.Versions), "versão", "versões")+" · "+
		plural(warns, "aviso", "avisos")+" · "+plural(errs, "erro", "erros")))

	if len(s.Warnings) == 0 {
		fmt.Fprintf(w, "\n%s Nenhum problema encontrado.\n", p.Green("✓"))
		return
	}
	writeWarningList(w, p, s.Warnings)
}

// Counts summarises warnings by severity.
func Counts(warnings []core.Warning) (warns, errs int) {
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

func shortPath(path string) string {
	if path == "" {
		return "php não encontrado no PATH"
	}
	return path
}

// wrapString greedily wraps text on spaces up to width runes.
func wrapString(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	current := ""
	for _, word := range words {
		switch {
		case current == "":
			current = word
		case runeLen(current)+1+runeLen(word) <= width:
			current += " " + word
		default:
			lines = append(lines, current)
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func runeLen(s string) int { return len([]rune(s)) }

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
