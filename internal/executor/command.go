package executor

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// CommandRunner executes an already-validated argv. It never invokes a shell.
type CommandRunner interface {
	Run(ctx context.Context, argv []string, stdout, stderr io.Writer) error
}

// OSCommandRunner runs commands directly through exec.CommandContext.
type OSCommandRunner struct{}

func (OSCommandRunner) Run(ctx context.Context, argv []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = commandEnv()
	cmd.Stdin = nil
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = 3 * time.Second
	return cmd.Run()
}

func commandEnv() []string {
	overrides := map[string]string{
		"LC_ALL":                   "C",
		"LANG":                     "C",
		"DEBIAN_FRONTEND":          "noninteractive",
		"APT_LISTCHANGES_FRONTEND": "none",
		"SYSTEMD_PAGER":            "cat",
		"SYSTEMD_COLORS":           "0",
	}
	var env []string
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, replaced := overrides[key]; replaced {
				continue
			}
		}
		env = append(env, entry)
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			return code
		}
	}
	if errors.Is(err, context.Canceled) {
		return 130
	}
	return 1
}
