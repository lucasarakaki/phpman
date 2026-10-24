package executor

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lucasarakaki/phpman/internal/core"
)

type fakePriv struct {
	steps         []core.Step
	rollbackSteps []core.Step
	err           error
	rollbackErr   error
	failStep      string
}

func (f *fakePriv) Run(_ context.Context, step core.Step) error {
	f.steps = append(f.steps, step)
	if f.failStep != "" && step.ID == f.failStep {
		return errors.New("falhou")
	}
	return f.err
}

func (f *fakePriv) RunRollback(_ context.Context, step core.Step) error {
	f.rollbackSteps = append(f.rollbackSteps, step)
	return f.rollbackErr
}

func (f *fakePriv) Close() error { return nil }

type fakeVerifier struct {
	steps []core.Step
	err   error
}

func (f *fakeVerifier) Verify(_ context.Context, step core.Step) error {
	f.steps = append(f.steps, step)
	return f.err
}

func TestExecutorRunsAndVerifiesStepsInOrder(t *testing.T) {
	plan := &core.Plan{
		Schema:  core.PlanSchema,
		Action:  core.ActionInstall,
		Version: "8.3",
		Steps: []core.Step{
			{ID: "install", Argv: []string{"apt-get", "install", "-y", "php8.3-cli"}, NeedsRoot: true, Verify: core.VerifyPackagesInstalled},
			{ID: "verify", Verify: core.VerifyBinaryVersion},
		},
	}
	priv := &fakePriv{}
	verifier := &fakeVerifier{}
	var out, errOut bytes.Buffer

	ex := &Executor{Priv: priv, Verifier: verifier, Out: &out, Err: &errOut}
	if err := ex.Run(context.Background(), plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(priv.steps) != 1 || priv.steps[0].ID != "install" {
		t.Errorf("passos privilegiados = %+v", priv.steps)
	}
	if len(verifier.steps) != 2 || verifier.steps[0].ID != "install" || verifier.steps[1].ID != "verify" {
		t.Errorf("verificações = %+v", verifier.steps)
	}
	if !strings.Contains(out.String(), "plano concluído") {
		t.Errorf("saída = %q", out.String())
	}
}

func TestExecutorStopsOnCommandFailure(t *testing.T) {
	plan := &core.Plan{Steps: []core.Step{
		{ID: "install", Argv: []string{"apt-get", "install", "-y", "php8.3-cli"}, NeedsRoot: true, Verify: core.VerifyPackagesInstalled},
		{ID: "next", Argv: []string{"systemctl", "start", "php8.3-fpm.service"}, NeedsRoot: true},
	}}
	priv := &fakePriv{err: errors.New("falhou")}
	verifier := &fakeVerifier{}
	ex := &Executor{Priv: priv, Verifier: verifier}

	err := ex.Run(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "install") {
		t.Fatalf("err = %v", err)
	}
	if len(priv.steps) != 1 {
		t.Errorf("executor continuou após falha: %+v", priv.steps)
	}
	if len(verifier.steps) != 0 {
		t.Errorf("verificação rodou após falha: %+v", verifier.steps)
	}
}

func TestExecutorRunsRollbackAfterFailure(t *testing.T) {
	plan := &core.Plan{
		Schema:  core.PlanSchema,
		Action:  core.ActionInstall,
		Version: "8.3",
		Steps: []core.Step{
			{ID: "install", Argv: []string{"apt-get", "install", "-y", "php8.3-cli"}, NeedsRoot: true},
			{ID: "enable", Argv: []string{"systemctl", "enable", "--now", "php8.3-fpm.service"}, NeedsRoot: true},
		},
		Rollback: []core.Step{
			{ID: "rollback-stop", Argv: []string{"systemctl", "disable", "--now", "php8.3-fpm.service"}, NeedsRoot: true},
			{ID: "rollback-remove", Argv: []string{"apt-get", "purge", "-y", "php8.3-cli"}, NeedsRoot: true},
		},
	}
	priv := &fakePriv{failStep: "enable"}
	var out, errOut bytes.Buffer
	ex := &Executor{Priv: priv, Verifier: &fakeVerifier{}, Out: &out, Err: &errOut}

	err := ex.Run(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "rollback automático executado") {
		t.Fatalf("err = %v", err)
	}
	if len(priv.steps) != 2 {
		t.Fatalf("passos normais = %+v", priv.steps)
	}
	if len(priv.rollbackSteps) != 2 || priv.rollbackSteps[0].ID != "rollback-stop" || priv.rollbackSteps[1].ID != "rollback-remove" {
		t.Errorf("rollback = %+v", priv.rollbackSteps)
	}
	if !strings.Contains(out.String(), "Rollback automático") || !strings.Contains(out.String(), "rollback concluído") {
		t.Errorf("saída = %q", out.String())
	}
}

func TestExecutorReportsRollbackFailure(t *testing.T) {
	plan := &core.Plan{
		Schema:  core.PlanSchema,
		Action:  core.ActionInstall,
		Version: "8.3",
		Steps: []core.Step{
			{ID: "install", Argv: []string{"apt-get", "install", "-y", "php8.3-cli"}, NeedsRoot: true},
		},
		Rollback: []core.Step{
			{ID: "rollback-remove", Argv: []string{"apt-get", "purge", "-y", "php8.3-cli"}, NeedsRoot: true},
		},
	}
	priv := &fakePriv{failStep: "install", rollbackErr: errors.New("sem rede")}
	var errOut bytes.Buffer
	ex := &Executor{Priv: priv, Verifier: &fakeVerifier{}, Err: &errOut}

	err := ex.Run(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "rollback automático incompleto") || !strings.Contains(err.Error(), "rollback-remove") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(errOut.String(), "aviso: rollback rollback-remove falhou") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestExecutorSkipsRollbackOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan := &core.Plan{
		Schema:  core.PlanSchema,
		Action:  core.ActionInstall,
		Version: "8.3",
		Steps: []core.Step{
			{ID: "install", Argv: []string{"apt-get", "install", "-y", "php8.3-cli"}, NeedsRoot: true},
		},
		Rollback: []core.Step{
			{ID: "rollback-remove", Argv: []string{"apt-get", "purge", "-y", "php8.3-cli"}, NeedsRoot: true},
		},
	}
	priv := &fakePriv{}
	ex := &Executor{Priv: priv, Verifier: &fakeVerifier{}}

	if err := ex.Run(ctx, plan); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, esperado context.Canceled", err)
	}
	if len(priv.steps) != 0 || len(priv.rollbackSteps) != 0 {
		t.Errorf("executou passos com contexto cancelado: normal=%+v rollback=%+v", priv.steps, priv.rollbackSteps)
	}
}

func TestExecutorStopsOnRequiredVerificationFailure(t *testing.T) {
	plan := &core.Plan{Steps: []core.Step{
		{ID: "verify", Verify: core.VerifyBinaryVersion},
		{ID: "next", Argv: []string{"systemctl", "start", "php8.3-fpm.service"}, NeedsRoot: true},
	}}
	priv := &fakePriv{}
	verifier := &fakeVerifier{err: errors.New("versão errada")}
	ex := &Executor{Priv: priv, Verifier: verifier}

	if err := ex.Run(context.Background(), plan); err == nil {
		t.Fatal("esperava erro de verificação")
	}
	if len(priv.steps) != 0 {
		t.Errorf("executor continuou após verificação obrigatória falhar: %+v", priv.steps)
	}
}

func TestExecutorWarnsOnOptionalVerificationFailure(t *testing.T) {
	plan := &core.Plan{Steps: []core.Step{
		{ID: "socket", Verify: core.VerifySocketExists, Optional: true},
		{ID: "next", Argv: []string{"systemctl", "start", "php8.3-fpm.service"}, NeedsRoot: true},
	}}
	priv := &fakePriv{}
	verifier := &fakeVerifier{err: errors.New("socket ausente")}
	var errOut bytes.Buffer
	ex := &Executor{Priv: priv, Verifier: verifier, Err: &errOut}

	if err := ex.Run(context.Background(), plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(priv.steps) != 1 || priv.steps[0].ID != "next" {
		t.Errorf("passos = %+v", priv.steps)
	}
	if !strings.Contains(errOut.String(), "opcional") {
		t.Errorf("aviso = %q", errOut.String())
	}
}

func TestNeedsHelper(t *testing.T) {
	plan := &core.Plan{Steps: []core.Step{
		{ID: "verify", Verify: core.VerifyBinaryVersion},
	}}
	if NeedsHelper(plan) {
		t.Error("plano só de verificação não precisa de helper")
	}
	plan.Steps = append(plan.Steps, core.Step{ID: "install", Argv: []string{"apt-get", "install", "-y", "php8.3-cli"}, NeedsRoot: true})
	if !NeedsHelper(plan) {
		t.Error("plano com passo root precisa de helper")
	}

	rollbackOnly := &core.Plan{Steps: []core.Step{
		{ID: "verify", Verify: core.VerifyBinaryVersion},
	}, Rollback: []core.Step{
		{ID: "rollback", Argv: []string{"apt-get", "purge", "-y", "php8.3-cli"}, NeedsRoot: true},
	}}
	if !NeedsHelper(rollbackOnly) {
		t.Error("plano com rollback root precisa de helper")
	}
}
