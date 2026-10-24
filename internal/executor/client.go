package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/lucasarakaki/phpman/internal/core"
)

const helperConnectTimeout = 2 * time.Minute

// PrivilegedRunner executes plan steps through the root helper. Rollback steps
// use a separate phase so the helper can enforce their order independently.
type PrivilegedRunner interface {
	Run(ctx context.Context, step core.Step) error
	RunRollback(ctx context.Context, step core.Step) error
	Close() error
}

// Helper is the client side of one sudo session. The JSON-lines protocol runs
// over a private Unix socket so sudo's pty handling cannot echo or translate
// protocol bytes. The helper process owns the password prompt; phpman never
// reads or stores the password.
type Helper struct {
	cmd    *exec.Cmd
	conn   net.Conn
	enc    *json.Encoder
	dec    *json.Decoder
	stdout io.Writer
	stderr io.Writer
	waitCh <-chan error
	dir    string
	closed bool
}

type acceptResult struct {
	conn net.Conn
	err  error
}

// StartSudoHelper launches the root helper and sends the plan over a socket
// created for this execution only.
func StartSudoHelper(ctx context.Context, plan *core.Plan, stdout, stderr io.Writer) (*Helper, error) {
	if plan == nil {
		return nil, fmt.Errorf("plano ausente")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("não foi possível localizar o executável: %w", err)
	}
	if err := validateHelperBinary(executable); err != nil {
		return nil, err
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		return nil, fmt.Errorf("sudo não encontrado: %w", err)
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	dir, err := os.MkdirTemp("", "phpman-helper-")
	if err != nil {
		return nil, fmt.Errorf("não foi possível criar o diretório do helper: %w", err)
	}
	_ = os.Chmod(dir, 0o700)
	socketPath := filepath.Join(dir, helperSocketName)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("não foi possível abrir o socket do helper: %w", err)
	}
	_ = os.Chmod(socketPath, 0o600)

	cmd := exec.CommandContext(ctx, "sudo", "--", executable, "__helper", "--socket", socketPath)
	cmd.Stdin = nil
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = listener.Close()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("não foi possível iniciar o helper com sudo: %w", err)
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	acceptCh := make(chan acceptResult, 1)
	go func() {
		conn, err := listener.Accept()
		acceptCh <- acceptResult{conn: conn, err: err}
	}()

	timer := time.NewTimer(helperConnectTimeout)
	defer timer.Stop()

	var conn net.Conn
	select {
	case res := <-acceptCh:
		if res.err != nil {
			_ = listener.Close()
			_ = os.RemoveAll(dir)
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-waitCh
			return nil, fmt.Errorf("não foi possível aceitar o helper: %w", res.err)
		}
		conn = res.conn
	case err := <-waitCh:
		_ = listener.Close()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("o helper terminou antes de conectar (sudo recusou?): %w", err)
	case <-ctx.Done():
		_ = listener.Close()
		_ = os.RemoveAll(dir)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-waitCh
		return nil, ctx.Err()
	case <-timer.C:
		_ = listener.Close()
		_ = os.RemoveAll(dir)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-waitCh
		return nil, fmt.Errorf("tempo esgotado aguardando o helper conectar; o sudo pediu senha?")
	}
	_ = listener.Close()

	peer, err := peerUID(conn)
	if err != nil || peer != 0 {
		_ = conn.Close()
		_ = os.RemoveAll(dir)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-waitCh
		if err != nil {
			return nil, fmt.Errorf("não foi possível confirmar que o helper é root: %w", err)
		}
		return nil, fmt.Errorf("o helper conectou com uid %d, esperado root", peer)
	}

	h := &Helper{
		cmd:    cmd,
		conn:   conn,
		enc:    json.NewEncoder(conn),
		dec:    json.NewDecoder(conn),
		stdout: stdout,
		stderr: stderr,
		waitCh: waitCh,
		dir:    dir,
	}
	if err := h.send(request{Op: opPlan, Plan: plan}); err != nil {
		h.abort()
		return nil, fmt.Errorf("não foi possível enviar o plano ao helper: %w", err)
	}
	if err := h.waitReady(); err != nil {
		h.abort()
		return nil, err
	}
	return h, nil
}

func (h *Helper) send(req request) error {
	return h.enc.Encode(req)
}

func (h *Helper) waitReady() error {
	for {
		ev, err := h.next()
		if err != nil {
			return err
		}
		switch ev.Kind {
		case kindReady:
			return nil
		case kindError:
			return errors.New(ev.Error)
		case kindStdout, kindStderr:
			if err := h.writeOutput(ev); err != nil {
				return err
			}
		default:
			return fmt.Errorf("evento inesperado do helper antes do plano: %q", ev.Kind)
		}
	}
}

func (h *Helper) Run(ctx context.Context, step core.Step) error {
	return h.run(ctx, step, opExec)
}

func (h *Helper) RunRollback(ctx context.Context, step core.Step) error {
	return h.run(ctx, step, opRollback)
}

func (h *Helper) run(ctx context.Context, step core.Step, op string) error {
	if len(step.Argv) == 0 && step.File == nil {
		return fmt.Errorf("passo %q não tem comando nem arquivo para executar", step.ID)
	}
	if err := h.send(request{Op: op, ID: step.ID, Argv: step.Argv}); err != nil {
		return fmt.Errorf("não foi possível enviar o passo %s: %w", step.ID, err)
	}
	for {
		ev, err := h.next()
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return err
		}
		if ev.ID != "" && ev.ID != step.ID {
			return fmt.Errorf("evento do helper para o passo inesperado %q", ev.ID)
		}
		switch ev.Kind {
		case kindStdout, kindStderr:
			if err := h.writeOutput(ev); err != nil {
				return err
			}
		case kindExit:
			if ev.Exit == nil {
				return fmt.Errorf("helper não informou o código de saída do passo %s", step.ID)
			}
			if *ev.Exit != 0 {
				if ev.Error != "" {
					return fmt.Errorf("passo %s falhou (%d): %s", step.ID, *ev.Exit, ev.Error)
				}
				return fmt.Errorf("passo %s falhou com código %d", step.ID, *ev.Exit)
			}
			return nil
		case kindError:
			return errors.New(ev.Error)
		default:
			return fmt.Errorf("evento inesperado do helper: %q", ev.Kind)
		}
	}
}

func (h *Helper) next() (event, error) {
	var ev event
	if err := h.dec.Decode(&ev); err != nil {
		if errors.Is(err, io.EOF) {
			return ev, fmt.Errorf("helper encerrou a comunicação")
		}
		return ev, fmt.Errorf("resposta inválida do helper: %w", err)
	}
	return ev, nil
}

func (h *Helper) writeOutput(ev event) error {
	var w io.Writer
	if ev.Kind == kindStdout {
		w = h.stdout
	} else {
		w = h.stderr
	}
	if _, err := fmt.Fprintln(w, ev.Line); err != nil {
		return fmt.Errorf("não foi possível escrever a saída do helper: %w", err)
	}
	return nil
}

func (h *Helper) Close() error {
	if h.closed {
		return nil
	}
	h.closed = true
	_ = h.send(request{Op: opDone})
	_ = h.conn.Close()
	err := <-h.waitCh
	_ = os.RemoveAll(h.dir)
	return err
}

func (h *Helper) abort() {
	_ = h.conn.Close()
	if h.cmd.Process != nil {
		_ = h.cmd.Process.Kill()
	}
	<-h.waitCh
	_ = os.RemoveAll(h.dir)
}
