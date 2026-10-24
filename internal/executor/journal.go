package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/lucasarakaki/phpman/internal/core"
)

// JournalPath is written exclusively by the privileged helper.
const JournalPath = "/var/lib/phpman/journal.jsonl"

type journalEvent struct {
	Time    string      `json:"time"`
	Phase   string      `json:"phase"`
	Action  core.Action `json:"action"`
	Version string      `json:"version"`
	Step    string      `json:"step"`
	Argv    []string    `json:"argv,omitempty"`
	File    string      `json:"file,omitempty"`
	SHA256  string      `json:"sha256,omitempty"`
	Exit    *int        `json:"exit,omitempty"`
	Error   string      `json:"error,omitempty"`
}

type journal struct {
	path string
	mu   sync.Mutex
}

func newJournal(path string) (*journal, error) {
	if path == "" {
		return nil, fmt.Errorf("caminho do journal não definido")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("não foi possível criar %s: %w", dir, err)
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("o journal não pode ser um symlink: %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("não foi possível inspecionar o journal: %w", err)
	}
	return &journal{path: path}, nil
}

func (j *journal) write(ev journalEvent) error {
	ev.Time = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("não foi possível serializar o journal: %w", err)
	}
	data = append(data, '\n')

	j.mu.Lock()
	defer j.mu.Unlock()

	f, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY|syscall.O_NOFOLLOW, 0o640)
	if err != nil {
		return fmt.Errorf("não foi possível abrir o journal: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("não foi possível escrever no journal: %w", err)
	}
	return nil
}
