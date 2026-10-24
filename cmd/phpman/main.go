// Command phpman inspects and, in future milestones, manages PHP
// installations on Linux distributions.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucasarakaki/phpman/internal/core"
	"github.com/lucasarakaki/phpman/internal/detect"
	"github.com/lucasarakaki/phpman/internal/executor"
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
		cmdHelper(ctx, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "phpman: comando desconhecido %q\n\n", args[0])
		usage(os.Stderr)
		os.Exit(2)
	}
}

func cmdHelper(ctx context.Context, args []string) {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "phpman: o helper privilegiado só pode ser iniciado com sudo")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("__helper", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	socket := fs.String("socket", "", "socket Unix privado do cliente")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *socket == "" {
		fmt.Fprintln(os.Stderr, "phpman: uso interno inválido de __helper")
		os.Exit(2)
	}

	conn, err := executor.ConnectHelperSocket(*socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "phpman helper: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()
	if err := executor.ServeHelper(ctx, conn, conn, executor.JournalPath); err != nil {
		fmt.Fprintf(os.Stderr, "phpman helper: %v\n", err)
		os.Exit(1)
	}
}

func runTUI(ctx context.Context) {
	if os.Geteuid() == 0 {
		fmt.Fprintln(os.Stderr, "phpman: não execute a interface como root; ela pedirá sudo apenas ao aplicar um plano")
		os.Exit(2)
	}
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
	if os.Geteuid() == 0 {
		fmt.Fprintln(os.Stderr, "phpman: não execute install/remove/switch como root; o helper pedirá sudo quando necessário")
		os.Exit(2)
	}

	fs := flag.NewFlagSet(string(action), flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "gera o plano sem executar nada")
	asJSON := fs.Bool("json", false, "emite o plano em JSON; exige --dry-run")
	yes := fs.Bool("yes", false, "executa sem pedir confirmação")
	fs.BoolVar(yes, "y", false, "alias de --yes")
	thirdParty := fs.Bool("third-party", false, "permite adicionar o repositório Sury/ondrej quando a versão não está no catálogo atual")
	noColor := fs.Bool("no-color", false, "desativa cores")
	_ = fs.Parse(reorderPlanArgs(args))

	if fs.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "phpman: use phpman %s <X.Y> [--dry-run] [--yes] [--third-party]\n", action)
		os.Exit(2)
	}
	if *asJSON && !*dryRun {
		fmt.Fprintln(os.Stderr, "phpman: --json exige --dry-run; a saída JSON descreve o plano e não executa")
		os.Exit(2)
	}
	if *thirdParty && action != core.ActionInstall {
		fmt.Fprintln(os.Stderr, "phpman: --third-party só se aplica a install")
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
		opts := provider.DefaultOptions()
		opts.AllowThirdParty = *thirdParty
		plan, err = prov.PlanInstall(snap, raw, opts)
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
	if *dryRun {
		report.WritePlan(os.Stdout, plan, useColor(*noColor))
		return
	}
	report.WritePlanExecution(os.Stdout, plan, useColor(*noColor))
	if len(plan.Steps) == 0 {
		return
	}

	if !*yes {
		if !isTerminal(os.Stdin) {
			fmt.Fprintln(os.Stderr, "phpman: sem TTY para confirmar; use --yes para executar o plano")
			os.Exit(2)
		}
		ok, err := confirmPlan(os.Stdout, os.Stdin, plan.Repository != nil)
		if err != nil {
			fail(err)
		}
		if !ok {
			fmt.Fprintln(os.Stdout, "cancelado; nada foi alterado.")
			return
		}
	}

	fmt.Fprintln(os.Stdout, "\nExecutando plano...")
	var helper *executor.Helper
	var priv executor.PrivilegedRunner
	if executor.NeedsHelper(plan) {
		helper, err = executor.StartSudoHelper(ctx, plan, os.Stdout, os.Stderr)
		if err != nil {
			fail(err)
		}
		priv = helper
	}

	ex := &executor.Executor{
		Priv:     priv,
		Verifier: executor.SystemVerifier{},
		Local:    executor.OSCommandRunner{},
		Out:      os.Stdout,
		Err:      os.Stderr,
	}
	runErr := ex.Run(ctx, plan)
	if helper != nil {
		if closeErr := helper.Close(); runErr == nil && closeErr != nil {
			runErr = fmt.Errorf("encerrando o helper: %w", closeErr)
		}
	}
	if runErr != nil {
		fail(runErr)
	}
}

func confirmPlan(w io.Writer, r io.Reader, thirdParty bool) (bool, error) {
	prompt := "\nExecutar este plano? [s/N] "
	if thirdParty {
		prompt = "\nAdicionar o repositório Sury/ondrej e executar este plano? [s/N] "
	}
	fmt.Fprint(w, prompt)
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "s", "sim", "y", "yes":
		return true, nil
	default:
		return false, nil
	}
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
		provider.ErrThirdPartyUnsupported,
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
	fmt.Fprintf(w, `phpman %s — inventário e gerenciamento de PHP no Linux

Uso:
  phpman                            abre a interface interativa
  phpman status                     relatório completo do ambiente
  phpman status --json              inventário em JSON (para scripts)
  phpman doctor                     apenas diagnósticos e sugestões
  phpman install <X.Y> [--dry-run]  instala uma versão (pede sudo)
  phpman install <X.Y> --third-party
                                    permite adicionar o repositório Sury/ondrej
  phpman remove <X.Y> [--dry-run]   remove uma versão (pede sudo)
  phpman switch <X.Y> [--dry-run]   troca a versão ativa (pede sudo)
  phpman install <X.Y> --yes        executa sem confirmar
  phpman version                    mostra a versão
  phpman help                       mostra esta ajuda

Sem --dry-run, o plano é exibido e uma confirmação é pedida antes de qualquer
alteração. A interface e os comandos rodam como o usuário; apenas o helper
efêmero __helper roda como root, via sudo. Para executar planos, instale o
binário root-owned com "sudo make install" em /usr/local/bin.

O plano pode ser emitido em JSON com --dry-run --json.

Quando a versão não está no catálogo da distribuição, --third-party permite
planejar a inclusão do repositório Sury/ondrej. O plano mostra a URL, a URL da
chave, o keyring com signed-by e o fingerprint esperado; a confirmação menciona
explicitamente o repositório de terceiros.

Variáveis:
  NO_COLOR=1             desativa cores
`, version)
}
