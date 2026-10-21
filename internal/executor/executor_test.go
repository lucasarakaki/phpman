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
	steps []core.Step
	err   error
}

func (f *fakePriv) Run(_ context.Context, step core.Step) error {
	f.steps = append(f.steps, step)
	return f.err
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
}
