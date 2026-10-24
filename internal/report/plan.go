package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/lucasarakaki/phpman/internal/core"
)

// WritePlanJSON encodes a plan as indented JSON.
func WritePlanJSON(w io.Writer, plan *core.Plan) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(plan)
}

// WritePlan renders a dry-run plan. The banner makes it clear that no command
// has been executed.
func WritePlan(w io.Writer, plan *core.Plan, color bool) {
	writePlan(w, plan, color, "· simulação, nada será alterado", "")
}

// WritePlanExecution renders a plan that is about to run. It still shows the
// exact argv and the planned rollback, but does not pretend to be a dry-run.
func WritePlanExecution(w io.Writer, plan *core.Plan, color bool) {
	writePlan(w, plan, color, "· revise antes de executar", "Se um passo falhar, o rollback abaixo será executado automaticamente, best-effort.")
}

func writePlan(w io.Writer, plan *core.Plan, color bool, banner, rollbackNote string) {
	p := NewPainter(color)

	fmt.Fprintf(w, "%s %s\n", p.Bold("phpman plano"), p.Dim(banner))
	fmt.Fprintf(w, "%s\n", p.Bold(plan.Summary))
	fmt.Fprintf(w, "%s\n", p.Dim(fmt.Sprintf("ação %s · versão %s · requer root: %s",
		plan.Action, plan.Version, yesNo(plan.NeedsRoot))))

	if len(plan.Warnings) > 0 {
		section(w, p, "avisos")
		for _, warning := range plan.Warnings {
			fmt.Fprintf(w, "  %s %s\n", p.Yellow("!"), warning)
		}
	}

	section(w, p, "passos")
	if len(plan.Steps) == 0 {
		fmt.Fprintf(w, "  %s\n", p.Dim("nada a executar"))
	} else {
		writePlanSteps(w, p, plan.Steps)
	}

	if len(plan.Rollback) > 0 {
		section(w, p, "rollback")
		writePlanSteps(w, p, plan.Rollback)
		if rollbackNote != "" {
			fmt.Fprintf(w, "  %s\n", p.Dim(rollbackNote))
		}
	}
}

func writePlanSteps(w io.Writer, p Painter, steps []core.Step) {
	for i, step := range steps {
		fmt.Fprintf(w, "  %s %s\n", p.Bold(fmt.Sprintf("%d.", i+1)), step.Description)
		switch {
		case len(step.Argv) > 0:
			fmt.Fprintf(w, "     %s\n", p.Cyan(strings.Join(step.Argv, " ")))
		case step.Verify != "":
			verify := string(step.Verify)
			if len(step.VerifyArgs) > 0 {
				verify += " " + strings.Join(step.VerifyArgs, " ")
			}
			fmt.Fprintf(w, "     %s\n", p.Dim("verificação: "+verify))
		}
		if step.Optional {
			fmt.Fprintf(w, "     %s\n", p.Dim("(opcional)"))
		}
	}
}

func yesNo(v bool) string {
	if v {
		return "sim"
	}
	return "não"
}
