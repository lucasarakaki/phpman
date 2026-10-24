package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/lucasarakaki/phpman/internal/core"
)

// FileWriter is the narrow privileged file-write primitive used by the helper.
// Tests inject a fake implementation; production uses writeFileAtomic.
type FileWriter func(path, content string, mode os.FileMode) error

// ServeHelper is the privileged side of the JSON-lines protocol. The caller
// must already be root; the CLI checks that before invoking it.
func ServeHelper(ctx context.Context, in io.Reader, out io.Writer, journalPath string) error {
	return serveHelper(ctx, in, out, journalPath, OSCommandRunner{}, resolveArgv, writeFileAtomic)
}

func serveHelper(
	ctx context.Context,
	in io.Reader,
	out io.Writer,
	journalPath string,
	runner CommandRunner,
	resolve func([]string) ([]string, error),
	writeFile FileWriter,
) error {
	j, err := newJournal(journalPath)
	if err != nil {
		return err
	}

	dec := json.NewDecoder(in)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)

	var plan *core.Plan
	var steps, rollback map[string]core.Step
	var order, rollbackOrder []string
	next, rollbackNext := 0, 0
	rollbackStarted := false

	for {
		var req request
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("protocolo inválido: %w", err)
		}

		switch req.Op {
		case opPlan:
			if plan != nil {
				return reject(enc, req.ID, "plano já carregado nesta sessão")
			}
			if req.Plan == nil {
				return reject(enc, req.ID, "mensagem plan sem plano")
			}
			validatedSteps, validatedRollback, err := validatePlan(req.Plan)
			if err != nil {
				return reject(enc, req.ID, err.Error())
			}
			plan = req.Plan
			steps = validatedSteps
			rollback = validatedRollback
			order = executableOrder(plan.Steps)
			rollbackOrder = executableOrder(plan.Rollback)
			next, rollbackNext = 0, 0
			rollbackStarted = false
			if err := enc.Encode(event{Kind: kindReady}); err != nil {
				return err
			}

		case opExec, opRollback:
			if plan == nil || steps == nil {
				return reject(enc, req.ID, "execução antes de carregar o plano")
			}
			if req.Op == opExec && rollbackStarted {
				return reject(enc, req.ID, "passo normal após o início do rollback")
			}
			if req.Op == opRollback {
				rollbackStarted = true
			}
			phaseOrder, phaseSteps, phaseNext := order, steps, &next
			if req.Op == opRollback {
				phaseOrder, phaseSteps, phaseNext = rollbackOrder, rollback, &rollbackNext
			}
			if *phaseNext >= len(phaseOrder) || req.ID != phaseOrder[*phaseNext] {
				return reject(enc, req.ID, "passo fora da ordem do plano: "+req.ID)
			}
			step, ok := phaseSteps[req.ID]
			if !ok {
				return reject(enc, req.ID, "passo não pertence ao plano: "+req.ID)
			}
			if !sameArgv(req.Argv, step.Argv) {
				return reject(enc, req.ID, "argv diverge do plano para o passo "+req.ID)
			}
			if err := executeStep(ctx, enc, j, runner, resolve, writeFile, plan, step, req.ID); err != nil {
				return err
			}
			*phaseNext = *phaseNext + 1

		case opDone:
			return nil

		default:
			return reject(enc, req.ID, "operação desconhecida: "+req.Op)
		}
	}
}

func executableOrder(steps []core.Step) []string {
	order := make([]string, 0, len(steps))
	for _, step := range steps {
		if len(step.Argv) > 0 || step.File != nil {
			order = append(order, step.ID)
		}
	}
	return order
}

func reject(enc *json.Encoder, id, message string) error {
	if err := enc.Encode(event{Kind: kindError, ID: id, Error: message}); err != nil {
		return err
	}
	return errors.New(message)
}

func executeStep(
	ctx context.Context,
	enc *json.Encoder,
	j *journal,
	runner CommandRunner,
	resolve func([]string) ([]string, error),
	writeFile FileWriter,
	plan *core.Plan,
	step core.Step,
	id string,
) error {
	if step.File != nil {
		return executeFileStep(ctx, enc, j, writeFile, plan, step, id)
	}

	canonical, err := resolve(step.Argv)
	if err != nil {
		return reject(enc, id, err.Error())
	}

	start := journalEvent{
		Phase:   "start",
		Action:  plan.Action,
		Version: plan.Version,
		Step:    step.ID,
		Argv:    canonical,
	}
	if err := j.write(start); err != nil {
		return reject(enc, id, err.Error())
	}

	var mu sync.Mutex
	emit := func(stream, line string) error {
		mu.Lock()
		defer mu.Unlock()
		return enc.Encode(event{Kind: stream, ID: id, Line: line})
	}
	stdout := newStreamWriter(kindStdout, emit)
	stderr := newStreamWriter(kindStderr, emit)

	runErr := runner.Run(ctx, canonical, stdout, stderr)
	flushErr := firstError(stdout.Flush(), stderr.Flush())

	code := exitCode(runErr)
	errText := ""
	if runErr != nil {
		errText = runErr.Error()
	}
	finish := journalEvent{
		Phase:   "finish",
		Action:  plan.Action,
		Version: plan.Version,
		Step:    step.ID,
		Argv:    canonical,
		Exit:    &code,
		Error:   errText,
	}
	if err := j.write(finish); err != nil {
		return reject(enc, id, err.Error())
	}
	if flushErr != nil {
		return reject(enc, id, flushErr.Error())
	}
	return enc.Encode(event{Kind: kindExit, ID: id, Exit: &code, Error: errText})
}

func executeFileStep(
	ctx context.Context,
	enc *json.Encoder,
	j *journal,
	writeFile FileWriter,
	plan *core.Plan,
	step core.Step,
	id string,
) error {
	if step.File == nil {
		return reject(enc, id, "passo de arquivo sem conteúdo")
	}
	if err := ctx.Err(); err != nil {
		return reject(enc, id, err.Error())
	}

	sum := sha256.Sum256([]byte(step.File.Content))
	hash := hex.EncodeToString(sum[:])
	start := journalEvent{
		Phase:   "start",
		Action:  plan.Action,
		Version: plan.Version,
		Step:    step.ID,
		File:    step.File.Path,
		SHA256:  hash,
	}
	if err := j.write(start); err != nil {
		return reject(enc, id, err.Error())
	}

	writeErr := writeFile(step.File.Path, step.File.Content, os.FileMode(step.File.Mode))
	code := exitCode(writeErr)
	errText := ""
	if writeErr != nil {
		errText = writeErr.Error()
	}
	finish := journalEvent{
		Phase:   "finish",
		Action:  plan.Action,
		Version: plan.Version,
		Step:    step.ID,
		File:    step.File.Path,
		SHA256:  hash,
		Exit:    &code,
		Error:   errText,
	}
	if err := j.write(finish); err != nil {
		return reject(enc, id, err.Error())
	}
	return enc.Encode(event{Kind: kindExit, ID: id, Exit: &code, Error: errText})
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

type streamWriter struct {
	stream string
	emit   func(string, string) error
	buf    []byte
}

func newStreamWriter(stream string, emit func(string, string) error) *streamWriter {
	return &streamWriter{stream: stream, emit: emit}
}

func (w *streamWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimSuffix(string(w.buf[:idx]), "\r")
		if err := w.emit(w.stream, line); err != nil {
			return len(p), err
		}
		w.buf = w.buf[idx+1:]
	}
	return len(p), nil
}

func (w *streamWriter) Flush() error {
	if len(w.buf) == 0 {
		return nil
	}
	line := strings.TrimSuffix(string(w.buf), "\r")
	w.buf = nil
	return w.emit(w.stream, line)
}
