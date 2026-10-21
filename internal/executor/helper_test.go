package executor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasarakaki/phpman/internal/core"
)

type fakeCommandRunner struct {
	run func(ctx context.Context, argv []string, stdout, stderr io.Writer) error
}

func (f fakeCommandRunner) Run(ctx context.Context, argv []string, stdout, stderr io.Writer) error {
	return f.run(ctx, argv, stdout, stderr)
}

func testPlan() *core.Plan {
	return &core.Plan{
		Schema:  core.PlanSchema,
		Action:  core.ActionInstall,
		Version: "8.3",
		Steps: []core.Step{{
			ID:        "install",
			Argv:      []string{"apt-get", "install", "-y", "php8.3-cli"},
			NeedsRoot: true,
			Verify:    core.VerifyPackagesInstalled,
		}},
	}
}

func startHelperServer(t *testing.T, plan *core.Plan, runner CommandRunner, resolve func([]string) ([]string, error)) (*json.Encoder, *json.Decoder, <-chan error) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	journalPath := filepath.Join(t.TempDir(), "journal.jsonl")

	go func() {
		err := serveHelper(context.Background(), inR, outW, journalPath, runner, resolve)
		_ = outW.Close()
		done <- err
	}()
	t.Cleanup(func() {
		_ = inW.Close()
		_ = outR.Close()
	})

	enc := json.NewEncoder(inW)
	dec := json.NewDecoder(outR)
	if err := enc.Encode(request{Op: opPlan, Plan: plan}); err != nil {
		t.Fatalf("enviar plano: %v", err)
	}
	var ready event
	if err := dec.Decode(&ready); err != nil {
		t.Fatalf("ler ready: %v", err)
	}
	if ready.Kind != kindReady {
		t.Fatalf("evento inicial = %+v, esperado ready", ready)
	}
	return enc, dec, done
}

func TestServeHelperExecutesPlanStep(t *testing.T) {
	plan := testPlan()
	var gotArgv []string
	runner := fakeCommandRunner{run: func(_ context.Context, argv []string, stdout, stderr io.Writer) error {
		gotArgv = append([]string(nil), argv...)
		_, _ = io.WriteString(stdout, "linha um\nlinha dois\n")
		_, _ = io.WriteString(stderr, "aviso\n")
		return nil
	}}
	resolve := func(argv []string) ([]string, error) {
		resolved := append([]string{"/usr/bin/" + argv[0]}, argv[1:]...)
		return resolved, nil
	}

	enc, dec, done := startHelperServer(t, plan, runner, resolve)
	if err := enc.Encode(request{Op: opExec, ID: "install", Argv: plan.Steps[0].Argv}); err != nil {
		t.Fatalf("enviar exec: %v", err)
	}

	var stdout, stderr []string
	var exit *int
	for exit == nil {
		var ev event
		if err := dec.Decode(&ev); err != nil {
			t.Fatalf("ler evento: %v", err)
		}
		switch ev.Kind {
		case kindStdout:
			stdout = append(stdout, ev.Line)
		case kindStderr:
			stderr = append(stderr, ev.Line)
		case kindExit:
			exit = ev.Exit
		case kindError:
			t.Fatalf("erro inesperado: %s", ev.Error)
		default:
			t.Fatalf("evento inesperado: %+v", ev)
		}
	}
	if exit == nil || *exit != 0 {
		t.Fatalf("exit = %v, esperado 0", exit)
	}
	if strings.Join(stdout, "|") != "linha um|linha dois" {
		t.Errorf("stdout = %#v", stdout)
	}
	if strings.Join(stderr, "|") != "aviso" {
		t.Errorf("stderr = %#v", stderr)
	}
	wantArgv := []string{"/usr/bin/apt-get", "install", "-y", "php8.3-cli"}
	if !sameArgv(gotArgv, wantArgv) {
		t.Errorf("argv executado = %#v, esperado %#v", gotArgv, wantArgv)
	}

	if err := enc.Encode(request{Op: opDone}); err != nil {
		t.Fatalf("enviar done: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("serveHelper: %v", err)
	}
}

func TestServeHelperRejectsArgvOutsidePlan(t *testing.T) {
	called := false
	runner := fakeCommandRunner{run: func(context.Context, []string, io.Writer, io.Writer) error {
		called = true
		return nil
	}}
	resolve := func(argv []string) ([]string, error) { return argv, nil }

	enc, dec, done := startHelperServer(t, testPlan(), runner, resolve)
	if err := enc.Encode(request{Op: opExec, ID: "install", Argv: []string{"apt-get", "purge", "-y", "php8.3-cli"}}); err != nil {
		t.Fatalf("enviar exec: %v", err)
	}
	var ev event
	if err := dec.Decode(&ev); err != nil {
		t.Fatalf("ler evento: %v", err)
	}
	if ev.Kind != kindError || !strings.Contains(ev.Error, "diverge") {
		t.Fatalf("evento = %+v, esperado erro de divergência", ev)
	}
	if err := <-done; err == nil {
		t.Error("serveHelper deveria terminar com erro")
	}
	if called {
		t.Error("comando foi executado apesar do argv divergente")
	}
}

func TestServeHelperReportsCommandFailure(t *testing.T) {
	runner := fakeCommandRunner{run: func(_ context.Context, _ []string, stdout, stderr io.Writer) error {
		_, _ = io.WriteString(stderr, "falhou\n")
		return errors.New("exit status 100")
	}}
	resolve := func(argv []string) ([]string, error) { return argv, nil }

	enc, dec, done := startHelperServer(t, testPlan(), runner, resolve)
	if err := enc.Encode(request{Op: opExec, ID: "install", Argv: testPlan().Steps[0].Argv}); err != nil {
		t.Fatalf("enviar exec: %v", err)
	}

	var ev event
	for {
		if err := dec.Decode(&ev); err != nil {
			t.Fatalf("ler evento: %v", err)
		}
		if ev.Kind == kindExit {
			break
		}
	}
	if ev.Exit == nil || *ev.Exit != 1 {
		t.Fatalf("exit = %v, esperado 1", ev.Exit)
	}
	if !strings.Contains(ev.Error, "exit status 100") {
		t.Errorf("erro = %q", ev.Error)
	}

	if err := enc.Encode(request{Op: opDone}); err != nil {
		t.Fatalf("enviar done: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("serveHelper: %v", err)
	}
}
