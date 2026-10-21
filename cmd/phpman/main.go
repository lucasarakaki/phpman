// Command phpman inspects and, in future milestones, manages PHP
// installations on Linux distributions.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucasarakaki/phpman/internal/core"
	"github.com/lucasarakaki/phpman/internal/detect"
	"github.com/lucasarakaki/phpman/internal/report"
	"github.com/lucasarakaki/phpman/internal/tui"
)

const scanTimeout = 45 * time.Second

var version = "0.1.0-dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	args := os.Args[1:]
	if len(args) == 0 {
		runTUI(ctx)
		return
	}

	switch args[0] {
	case "status":
		cmdStatus(ctx, args[1:])
	case "doctor":
		cmdDoctor(ctx, args[1:])
	case "version", "--version", "-v":
		fmt.Printf("phpman %s\n", version)
	case "help", "--help", "-h":
		usage(os.Stdout)
	case "__helper":
		fmt.Fprintln(os.Stderr, "phpman: o helper privilegiado ainda não existe (chega no M1)")
		os.Exit(2)
	default:
		fmt.Fprintf(os.Stderr, "phpman: comando desconhecido %q\n\n", args[0])
		usage(os.Stderr)
		os.Exit(2)
	}
}

func runTUI(ctx context.Context) {
	if !isTerminal(os.Stdout) || !isTerminal(os.Stdin) {
		snap, err := scan(ctx)
		if err != nil {
			fail(err)
		}
		report.WriteText(os.Stdout, snap, version, false)
		return
	}

	program := tea.NewProgram(tui.New(ctx, version), tea.WithAltScreen(), tea.WithContext(ctx))
	if _, err := program.Run(); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "phpman: %v\n", err)
		os.Exit(1)
	}
}

func cmdStatus(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "emite o inventário completo em JSON")
	noColor := fs.Bool("no-color", false, "desativa cores")
	_ = fs.Parse(args)

	snap, err := scan(ctx)
	if err != nil {
		fail(err)
	}
	if *asJSON {
		if err := report.WriteJSON(os.Stdout, snap); err != nil {
			fail(err)
		}
		return
	}
	report.WriteText(os.Stdout, snap, version, useColor(*noColor))
}

func cmdDoctor(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	noColor := fs.Bool("no-color", false, "desativa cores")
	_ = fs.Parse(args)

	snap, err := scan(ctx)
	if err != nil {
		fail(err)
	}
	report.WriteDoctor(os.Stdout, snap, useColor(*noColor))
}

func scan(ctx context.Context) (*core.Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()
	return detect.Scan(ctx)
}

func useColor(disabled bool) bool {
	if disabled || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return isTerminal(os.Stdout)
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "phpman: %v\n", err)
	os.Exit(1)
}

func usage(w *os.File) {
	fmt.Fprintf(w, `phpman %s — inventário e diagnóstico de PHP no Linux (M0, somente leitura)

Uso:
  phpman                 abre a interface interativa
  phpman status          relatório completo do ambiente
  phpman status --json   inventário em JSON (para scripts)
  phpman doctor          apenas diagnósticos e sugestões
  phpman version         mostra a versão
  phpman help            mostra esta ajuda

Nesta versão nada é alterado e nenhum acesso root é solicitado.
Instalação, remoção, troca de versão e extensões chegam nos próximos marcos.

Variáveis:
  NO_COLOR=1             desativa cores
`, version)
}
