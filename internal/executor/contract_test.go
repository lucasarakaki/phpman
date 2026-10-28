package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lucasarakaki/phpman/internal/core"
	"github.com/lucasarakaki/phpman/internal/report"
)

// contractPlan exercises every kind of executable step the helper supports:
// a root argv step, a root file write and a verification-only step. The
// rollback also contains an executable step so the test covers both phases.
func contractPlan() *core.Plan {
	return &core.Plan{
		Schema:    core.PlanSchema,
		Action:    core.ActionInstall,
		Version:   "8.3",
		Summary:   "plano de contrato",
		NeedsRoot: true,
		Steps: []core.Step{
			{
				ID:          "install-packages",
				Description: "Instalar php8.3-cli",
				Argv:        []string{"apt-get", "install", "-y", "php8.3-cli"},
				NeedsRoot:   true,
				Verify:      core.VerifyPackagesInstalled,
				VerifyArgs:  []string{"php8.3-cli"},
			},
			{
				ID:          "write-sury-source",
				Description: "Escrever o source list",
				File: &core.FileWrite{
					Path:    surySourceFile,
					Content: "deb [signed-by=" + suryKeyring + "] https://packages.sury.org/php/ bookworm main\n",
					Mode:    0o644,
				},
				NeedsRoot: true,
				Verify:    core.VerifyFileContent,
			},
			{
				ID:         "verify-binary",
				Verify:     core.VerifyBinaryVersion,
				VerifyArgs: []string{"/usr/bin/php8.3", "8.3"},
			},
		},
		Rollback: []core.Step{
			{
				ID:          "rollback-remove-packages",
				Description: "Remover php8.3-cli",
				Argv:        []string{"apt-get", "purge", "-y", "php8.3-cli"},
				NeedsRoot:   true,
				Verify:      core.VerifyPackagesAbsent,
				VerifyArgs:  []string{"php8.3-cli"},
			},
		},
	}
}

func startContractHelper(t *testing.T, plan *core.Plan, runner CommandRunner) (*json.Encoder, *json.Decoder, string, <-chan error) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	journalPath := filepath.Join(t.TempDir(), "journal.jsonl")
	done := make(chan error, 1)

	resolve := func(argv []string) ([]string, error) {
		resolved := append([]string{"/usr/bin/" + argv[0]}, argv[1:]...)
		return resolved, nil
	}
	writeFile := func(string, string, os.FileMode) error { return nil }

	go func() {
		err := serveHelper(context.Background(), inR, outW, journalPath, runner, resolve, writeFile)
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
	return enc, dec, journalPath, done
}

func runContractStep(t *testing.T, enc *json.Encoder, dec *json.Decoder, id string, argv []string) int {
	t.Helper()
	if err := enc.Encode(request{Op: opExec, ID: id, Argv: argv}); err != nil {
		t.Fatalf("enviar passo %s: %v", id, err)
	}
	for {
		var ev event
		if err := dec.Decode(&ev); err != nil {
			t.Fatalf("ler passo %s: %v", id, err)
		}
		switch ev.Kind {
		case kindStdout, kindStderr:
			continue
		case kindExit:
			if ev.Exit == nil {
				t.Fatalf("passo %s sem código de saída", id)
			}
			return *ev.Exit
		case kindError:
			t.Fatalf("passo %s rejeitado: %s", id, ev.Error)
		default:
			t.Fatalf("evento inesperado no passo %s: %+v", id, ev)
		}
	}
}

func runContractRollback(t *testing.T, enc *json.Encoder, dec *json.Decoder, id string, argv []string) int {
	t.Helper()
	if err := enc.Encode(request{Op: opRollback, ID: id, Argv: argv}); err != nil {
		t.Fatalf("enviar rollback %s: %v", id, err)
	}
	for {
		var ev event
		if err := dec.Decode(&ev); err != nil {
			t.Fatalf("ler rollback %s: %v", id, err)
		}
		switch ev.Kind {
		case kindStdout, kindStderr:
			continue
		case kindExit:
			if ev.Exit == nil {
				t.Fatalf("rollback %s sem código de saída", id)
			}
			return *ev.Exit
		case kindError:
			t.Fatalf("rollback %s rejeitado: %s", id, ev.Error)
		default:
			t.Fatalf("evento inesperado no rollback %s: %+v", id, ev)
		}
	}
}

func readJournal(t *testing.T, path string) []journalEvent {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ler journal: %v", err)
	}
	var events []journalEvent
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var ev journalEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("journal inválido: %v", err)
		}
		events = append(events, ev)
	}
	return events
}

// TestDryRunPlanMatchesHelperExecution is the contract between the dry-run JSON
// and the privileged execution: serializing a plan, decoding it and executing
// it must produce the same executable steps and argv in the same order.
func TestDryRunPlanMatchesHelperExecution(t *testing.T) {
	plan := contractPlan()

	var dryRun strings.Builder
	if err := report.WritePlanJSON(&dryRun, plan); err != nil {
		t.Fatalf("WritePlanJSON: %v", err)
	}
	var decoded core.Plan
	if err := json.Unmarshal([]byte(dryRun.String()), &decoded); err != nil {
		t.Fatalf("decodificar dry-run: %v", err)
	}
	if !reflect.DeepEqual(plan, &decoded) {
		t.Fatalf("plano mudou no round-trip JSON\noriginal: %+v\ndecodificado: %+v", plan, &decoded)
	}

	runner := fakeCommandRunner{run: func(_ context.Context, _ []string, stdout, stderr io.Writer) error {
		_, _ = io.WriteString(stdout, "ok\n")
		_, _ = io.WriteString(stderr, "aviso\n")
		return nil
	}}
	enc, dec, journalPath, done := startContractHelper(t, &decoded, runner)

	for _, step := range decoded.Steps {
		if len(step.Argv) == 0 && step.File == nil {
			continue
		}
		if code := runContractStep(t, enc, dec, step.ID, step.Argv); code != 0 {
			t.Fatalf("passo %s terminou com código %d", step.ID, code)
		}
	}
	if err := enc.Encode(request{Op: opDone}); err != nil {
		t.Fatalf("enviar done: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("serveHelper: %v", err)
	}

	events := readJournal(t, journalPath)
	expected := make([]core.Step, 0, len(decoded.Steps))
	for _, step := range decoded.Steps {
		if len(step.Argv) > 0 || step.File != nil {
			expected = append(expected, step)
		}
	}

	var starts []journalEvent
	for _, ev := range events {
		if ev.Phase == "start" {
			starts = append(starts, ev)
		}
	}
	if len(starts) != len(expected) {
		t.Fatalf("journal tem %d inícios, esperado %d: %+v", len(starts), len(expected), starts)
	}
	for i, step := range expected {
		ev := starts[i]
		if ev.Step != step.ID {
			t.Errorf("passo %d no journal = %q, esperado %q", i, ev.Step, step.ID)
		}
		if len(step.Argv) > 0 {
			want := append([]string{"/usr/bin/" + step.Argv[0]}, step.Argv[1:]...)
			if !sameArgv(ev.Argv, want) {
				t.Errorf("argv do journal para %s = %#v, esperado %#v", step.ID, ev.Argv, want)
			}
			if ev.File != "" {
				t.Errorf("passo de comando %s registrou arquivo %q", step.ID, ev.File)
			}
			continue
		}
		if step.File == nil {
			t.Fatalf("passo %s não é executável", step.ID)
		}
		if ev.File != step.File.Path {
			t.Errorf("arquivo do journal para %s = %q, esperado %q", step.ID, ev.File, step.File.Path)
		}
		sum := sha256.Sum256([]byte(step.File.Content))
		if ev.SHA256 != hex.EncodeToString(sum[:]) {
			t.Errorf("sha256 do journal para %s = %q", step.ID, ev.SHA256)
		}
		if len(ev.Argv) != 0 {
			t.Errorf("passo de arquivo %s registrou argv %#v", step.ID, ev.Argv)
		}
	}

	assertJournalStartsHaveFinish(t, events)
	if strings.Contains(strings.ToLower(strings.Join(journalText(events), "\n")), "password") {
		t.Error("journal contém a palavra password")
	}
}

// TestHelperJournalRecordsFailureAndRollback checks that a failed step and the
// rollback phase leave a balanced journal: one start and one finish per
// executable step, with the failed step's exit code preserved.
func TestHelperJournalRecordsFailureAndRollback(t *testing.T) {
	plan := contractPlan()
	runner := fakeCommandRunner{run: func(_ context.Context, argv []string, _, _ io.Writer) error {
		if len(argv) > 1 && argv[1] == "install" {
			return errors.New("falha simulada")
		}
		return nil
	}}
	enc, dec, journalPath, done := startContractHelper(t, plan, runner)

	if code := runContractStep(t, enc, dec, plan.Steps[0].ID, plan.Steps[0].Argv); code == 0 {
		t.Fatal("passo de instalação deveria falhar")
	}
	if code := runContractRollback(t, enc, dec, plan.Rollback[0].ID, plan.Rollback[0].Argv); code != 0 {
		t.Fatalf("rollback terminou com código %d", code)
	}
	if err := enc.Encode(request{Op: opDone}); err != nil {
		t.Fatalf("enviar done: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("serveHelper: %v", err)
	}

	events := readJournal(t, journalPath)
	if len(events) != 4 {
		t.Fatalf("eventos do journal = %d, esperado 4: %+v", len(events), events)
	}
	if events[0].Step != "install-packages" || events[0].Phase != "start" {
		t.Errorf("primeiro evento = %+v", events[0])
	}
	if events[1].Step != "install-packages" || events[1].Phase != "finish" || events[1].Exit == nil || *events[1].Exit == 0 {
		t.Errorf("falha não registrada corretamente: %+v", events[1])
	}
	if events[2].Step != "rollback-remove-packages" || events[2].Phase != "start" {
		t.Errorf("início do rollback = %+v", events[2])
	}
	if events[3].Step != "rollback-remove-packages" || events[3].Phase != "finish" || events[3].Exit == nil || *events[3].Exit != 0 {
		t.Errorf("fim do rollback = %+v", events[3])
	}
	assertJournalStartsHaveFinish(t, events)
}

func assertJournalStartsHaveFinish(t *testing.T, events []journalEvent) {
	t.Helper()
	starts := map[string]int{}
	finishes := map[string]int{}
	for _, ev := range events {
		switch ev.Phase {
		case "start":
			starts[ev.Step]++
		case "finish":
			finishes[ev.Step]++
		}
	}
	for step, count := range starts {
		if finishes[step] != count {
			t.Errorf("passo %s tem %d start(s) e %d finish(es)", step, count, finishes[step])
		}
	}
	for step, count := range finishes {
		if starts[step] != count {
			t.Errorf("passo %s tem %d finish(es) e %d start(s)", step, count, starts[step])
		}
	}
}

func journalText(events []journalEvent) []string {
	lines := make([]string, 0, len(events))
	for _, ev := range events {
		data, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		lines = append(lines, string(data))
	}
	return lines
}
