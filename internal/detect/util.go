package detect

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// defaultTimeout bounds every external command. Detection must never hang the
// CLI because a package manager is locked or a unit is stuck.
const defaultTimeout = 5 * time.Second

// run executes a command and returns its combined output. A non-zero exit
// status is returned as an error together with the output: several tools we
// query (systemctl is-active, update-alternatives) use the exit code to carry
// state rather than failure.
func run(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	if _, err := exec.LookPath(name); err != nil {
		return "", err
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()

	return strings.TrimSpace(string(out)), err
}

// hasCommand reports whether an executable is available in PATH.
func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// commandError reports whether err means "tool not installed" as opposed to a
// real failure worth surfacing.
func commandError(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist)
}

// exists reports whether a path is present (any file type).
func exists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// isExecutable reports whether path is a regular file (or symlink to one) with
// at least one execute bit set.
func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode().Perm()&0o111 != 0
}

// expandGlob runs filepath.Glob and swallows ErrBadPattern.
func expandGlob(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return matches
}

// unquote removes a matching pair of single or double quotes.
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// firstLine returns the first non-empty line of s.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// appendNote adds a note once.
func appendNote(notes []string, note string) []string {
	note = strings.TrimSpace(note)
	if note == "" {
		return notes
	}
	for _, n := range notes {
		if n == note {
			return notes
		}
	}
	return append(notes, note)
}
