package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelperClientProtocol(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverCh := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverCh <- err
			return
		}
		defer conn.Close()
		runner := fakeCommandRunner{run: func(_ context.Context, _ []string, stdout, stderr io.Writer) error {
			_, _ = io.WriteString(stdout, "saída do comando\n")
			_, _ = io.WriteString(stderr, "aviso do comando\n")
			return nil
		}}
		resolve := func(argv []string) ([]string, error) { return argv, nil }
		serverCh <- serveHelper(context.Background(), conn, conn, filepath.Join(t.TempDir(), "journal.jsonl"), runner, resolve)
	}()

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- <-serverCh }()

	var stdout, stderr bytes.Buffer
	dir := filepath.Join(t.TempDir(), "helper")
	h := &Helper{
		conn:   conn,
		enc:    json.NewEncoder(conn),
		dec:    json.NewDecoder(conn),
		stdout: &stdout,
		stderr: &stderr,
		waitCh: waitCh,
		dir:    dir,
	}

	plan := testPlan()
	if err := h.send(request{Op: opPlan, Plan: plan}); err != nil {
		t.Fatal(err)
	}
	if err := h.waitReady(); err != nil {
		t.Fatalf("waitReady: %v", err)
	}
	if err := h.Run(context.Background(), plan.Steps[0]); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "saída do comando" {
		t.Errorf("stdout = %q", got)
	}
	if got := strings.TrimSpace(stderr.String()); got != "aviso do comando" {
		t.Errorf("stderr = %q", got)
	}
}
