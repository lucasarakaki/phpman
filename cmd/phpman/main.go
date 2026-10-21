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
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucasarakaki/phpman/internal/core"
	"github.com/lucasarakaki/phpman/internal/detect"
	"github.com/lucasarakaki/phpman/internal/provider"
	"github.com/lucasarakaki/phpman/internal/provider/registry"
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
	case "install":
		cmdPlan(ctx, core.ActionInstall, args[1:])
	case "remove":
		cmdPlan(ctx, core.ActionRemove, args[1:])
	case "switch":
		cmdPlan(ctx, core.ActionSwitch, args[1:])
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

func cmdPlan(ctx context.Context, action core.Action, args []string) {
	fs := flag.NewFlagSet(string(action), flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "gera o plano sem executar nada")
	asJSON := fs.Bool("json", false, "emite o plano em JSON")
	noColor := fs.Bool("no-color", false, "desativa cores")
	_ = fs.Parse(reorderPlanArgs(args))

	if fs.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "phpman: use phpman %s <X.Y> --dry-run\n", action)
		os.Exit(2)
	}
	if !*dryRun {
		fmt.Fprintln(os.Stderr, "phpman: a execução real chega no M1.2; use --dry-run para ver o plano")
		os.Exit(2)
	}

	raw := fs.Arg(0)
	snap, err := scan(ctx)
	if err != nil {
		fail(err)
	}
	prov, err := registry.For(snap.Distro)
	if err != nil {
		failPlan(err)
	}

	var plan *core.Plan
	switch action {
	case core.ActionInstall:
		plan, err = prov.PlanInstall(snap, raw, provider.DefaultOptions())
	case core.ActionRemove:
		plan, err = prov.PlanRemove(snap, raw)
	case core.ActionSwitch:
		plan, err = prov.PlanSwitch(snap, raw)
	default:
		err = fmt.Errorf("ação desconhecida %q", action)
	}
	if err != nil {
		failPlan(err)
	}

	if *asJSON {
		if err := report.WritePlanJSON(os.Stdout, plan); err != nil {
			fail(err)
		}
		return
	}
	report.WritePlan(os.Stdout, plan, useColor(*noColor))
}

// reorderPlanArgs lets flags come before or after the version argument. The
// standard flag package stops parsing at the first positional argument.
func reorderPlanArgs(args []string) []string {
	flags := make([]string, 0, len(args))
	positional := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			continue
		}
		positional = append(positional, arg)
	}
	return append(flags, positional...)
}

func failPlan(err error) {
	for _, sentinel := range []error{
		provider.ErrUnsupportedDistro,
		provider.ErrInvalidVersion,
		provider.ErrVersionNotInstalled,
		provider.ErrVersionUnavailable,
		provider.ErrActiveVersion,
		provider.ErrNoPackages,
	} {
		if errors.Is(err, sentinel) {
			fmt.Fprintf(os.Stderr, "phpman: %v\n", err)
			os.Exit(2)
		}
	}
	fail(err)
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
	fmt.Fprintf(w, `phpman %s — inventário e diagnóstico de PHP no Linux

Uso:
  phpman                 abre a interface interativa
  phpman status          relatório completo do ambiente
  phpman status --json   inventário em JSON (para scripts)
  phpman doctor          apenas diagnósticos e sugestões
  phpman install <X.Y> --dry-run   simula a instalação de uma versão
  phpman remove <X.Y> --dry-run    simula a remoção de uma versão
  phpman switch <X.Y> --dry-run    simula a troca da versão ativa
  phpman version         mostra a versão
  phpman help            mostra esta ajuda

Nesta versão nada é alterado e nenhum acesso root é solicitado.
Os planos podem ser emitidos em JSON com --json. A execução real chega no M1.2.

Variáveis:
  NO_COLOR=1             desativa cores
`, version)
}
