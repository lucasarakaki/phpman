// Package executor applies a core.Plan through an allowlisted privileged
// helper. Planning and execution are deliberately separate: the helper
// validates every argv against the plan it received in memory.
package executor

import (
	"context"
	"fmt"
	"io"

	"github.com/lucasarakaki/phpman/internal/core"
)

// Executor runs plan steps in order, verifying each one before continuing.
type Executor struct {
	Priv     PrivilegedRunner
	Verifier Verifier
	Local    CommandRunner
	Out      io.Writer
	Err      io.Writer
}

// NeedsHelper reports whether a plan has any step that must run as root.
func NeedsHelper(plan *core.Plan) bool {
	if plan == nil {
		return false
	}
	for _, step := range plan.Steps {
		if len(step.Argv) > 0 && step.NeedsRoot {
			return true
		}
	}
	return false
}

// Run applies every step, stopping at the first failure. Rollback is planned
// but not executed here; M1.3 adds the automatic best-effort rollback flow.
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

		switch {
		case len(step.Argv) > 0 && step.NeedsRoot:
			if e.Priv == nil {
				return fmt.Errorf("passo %s exige root, mas o helper não foi iniciado", step.ID)
			}
			if err := e.Priv.Run(ctx, step); err != nil {
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
			continue
		}
		if err := e.Verifier.Verify(ctx, step); err != nil {
			if step.Optional {
				fmt.Fprintf(e.Err, "aviso: verificação opcional do passo %s falhou: %v\n", step.ID, err)
				continue
			}
			return fmt.Errorf("verificação do passo %s falhou: %w", step.ID, err)
		}
	}
	fmt.Fprintln(e.Out, "\nplano concluído.")
	return nil
}
