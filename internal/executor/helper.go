package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/lucasarakaki/phpman/internal/core"
)

// ServeHelper is the privileged side of the JSON-lines protocol. The caller
// must already be root; the CLI checks that before invoking it.
func ServeHelper(ctx context.Context, in io.Reader, out io.Writer, journalPath string) error {
	return serveHelper(ctx, in, out, journalPath, OSCommandRunner{}, resolveArgv)
}

func serveHelper(
	ctx context.Context,
	in io.Reader,
	out io.Writer,
	journalPath string,
	runner CommandRunner,
	resolve func([]string) ([]string, error),
) error {
	j, err := newJournal(journalPath)
	if err != nil {
		return err
	}

	dec := json.NewDecoder(in)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)

	var plan *core.Plan
	var steps map[string]core.Step
	var order []string
	next := 0

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
			validated, err := validatePlan(req.Plan)
			if err != nil {
				return reject(enc, req.ID, err.Error())
			}
			plan = req.Plan
			steps = validated
			order = executableOrder(plan)
			next = 0
			if err := enc.Encode(event{Kind: kindReady}); err != nil {
				return err
			}

		case opExec:
			if plan == nil || steps == nil {
				return reject(enc, req.ID, "exec antes de carregar o plano")
			}
			if next >= len(order) || req.ID != order[next] {
				return reject(enc, req.ID, "passo fora da ordem do plano: "+req.ID)
			}
			step, ok := steps[req.ID]
			if !ok {
				return reject(enc, req.ID, "passo não pertence ao plano: "+req.ID)
			}
			if !sameArgv(req.Argv, step.Argv) {
				return reject(enc, req.ID, "argv diverge do plano para o passo "+req.ID)
			}
			if err := executeStep(ctx, enc, j, runner, resolve, plan, step, req.ID); err != nil {
				return err
			}
			next++

		case opDone:
			return nil

		default:
			return reject(enc, req.ID, "operação desconhecida: "+req.Op)
		}
	}
}

func executableOrder(plan *core.Plan) []string {
	order := make([]string, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		if len(step.Argv) > 0 {
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
	plan *core.Plan,
	step core.Step,
	id string,
) error {
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
