// Package executor applies a core.Plan through an allowlisted privileged
// helper. Planning and execution are deliberately separate: the helper
// validates every argv against the plan it received in memory.
package executor

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/lucasarakaki/phpman/internal/core"
)

// Executor runs plan steps in order, verifying each one before continuing. On
// failure it executes the plan's rollback steps best-effort in the same helper
// session and reports both the original failure and any rollback failures.
type Executor struct {
	Priv     PrivilegedRunner
	Verifier Verifier
	Local    CommandRunner
	Out      io.Writer
	Err      io.Writer
}

// NeedsHelper reports whether a plan has any step or rollback step that must
// run as root.
func NeedsHelper(plan *core.Plan) bool {
	if plan == nil {
		return false
	}
	for _, step := range append(append([]core.Step{}, plan.Steps...), plan.Rollback...) {
		if len(step.Argv) > 0 && step.NeedsRoot {
			return true
		}
	}
	return false
}

// Run applies every step, stopping at the first failure. If the plan has a
// rollback, Run tries to execute it before returning the original error.
func (e *Executor) Run(ctx context.Context, plan *core.Plan) error {
	if plan == nil {
		return fmt.Errorf("plano ausente")
	}
	if e.Out == nil {
		e.Out = io.Discard
	}
	if e.Err == nil {
		e.Err = io.Discard
	}
	if e.Local == nil {
		e.Local = OSCommandRunner{}
	}
	if e.Verifier == nil {
		e.Verifier = SystemVerifier{}
	}

	for i, step := range plan.Steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		description := step.Description
		if description == "" {
			description = step.ID
		}
		fmt.Fprintf(e.Out, "\n[%d/%d] %s\n", i+1, len(plan.Steps), description)

		if err := e.executeStep(ctx, step, false); err != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(e.Err, "rollback automático não executado: execução interrompida.")
				return err
			}
			if len(plan.Rollback) == 0 {
				return err
			}
			if rollbackErr := e.rollback(ctx, plan.Rollback); rollbackErr != nil {
				return fmt.Errorf("%w; rollback automático incompleto: %v", err, rollbackErr)
			}
			return fmt.Errorf("%w; rollback automático executado", err)
		}
	}
	fmt.Fprintln(e.Out, "\nplano concluído.")
	return nil
}

// executeStep runs the command (if any) and its declarative verification. The
// rollback flag only selects the privileged protocol phase.
func (e *Executor) executeStep(ctx context.Context, step core.Step, rollback bool) error {
	switch {
	case len(step.Argv) > 0 && step.NeedsRoot:
		if e.Priv == nil {
			return fmt.Errorf("passo %s exige root, mas o helper não foi iniciado", step.ID)
		}
		var err error
		if rollback {
			err = e.Priv.RunRollback(ctx, step)
		} else {
			err = e.Priv.Run(ctx, step)
		}
		if err != nil {
			return fmt.Errorf("passo %s: %w", step.ID, err)
		}
	case len(step.Argv) > 0:
		if err := e.Local.Run(ctx, step.Argv, e.Out, e.Err); err != nil {
			return fmt.Errorf("passo %s: %w", step.ID, err)
		}
	case step.Verify == "":
		return fmt.Errorf("passo %s não tem comando nem verificação", step.ID)
	}

	if step.Verify == "" {
		return nil
	}
	if err := e.Verifier.Verify(ctx, step); err != nil {
		if step.Optional {
			fmt.Fprintf(e.Err, "aviso: verificação opcional do passo %s falhou: %v\n", step.ID, err)
			return nil
		}
		return fmt.Errorf("verificação do passo %s falhou: %w", step.ID, err)
	}
	return nil
}

func (e *Executor) rollback(ctx context.Context, steps []core.Step) error {
	if len(steps) == 0 {
		return nil
	}
	fmt.Fprintln(e.Out, "\nRollback automático...")

	var failures []string
	for i, step := range steps {
		if err := ctx.Err(); err != nil {
			failures = append(failures, "execução interrompida")
			break
		}
		description := step.Description
		if description == "" {
			description = step.ID
		}
		fmt.Fprintf(e.Out, "\n[rollback %d/%d] %s\n", i+1, len(steps), description)
		if err := e.executeStep(ctx, step, true); err != nil {
			fmt.Fprintf(e.Err, "aviso: rollback %s falhou: %v\n", step.ID, err)
			failures = append(failures, step.ID)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("passos de rollback com falha: %s", strings.Join(failures, ", "))
	}
	fmt.Fprintln(e.Out, "\nrollback concluído.")
	return nil
}
