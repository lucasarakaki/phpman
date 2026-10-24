package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasarakaki/phpman/internal/core"
)

func TestJournalWritesJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "journal.jsonl")
	j, err := newJournal(path)
	if err != nil {
		t.Fatalf("newJournal: %v", err)
	}
	code := 0
	if err := j.write(journalEvent{
		Phase:   "start",
		Action:  core.ActionInstall,
		Version: "8.3",
		Step:    "install",
		Argv:    []string{"/usr/bin/apt-get", "install", "-y", "php8.3-cli"},
	}); err != nil {
		t.Fatalf("write start: %v", err)
	}
	if err := j.write(journalEvent{
		Phase:   "finish",
		Action:  core.ActionInstall,
		Version: "8.3",
		Step:    "install",
		Argv:    []string{"/usr/bin/apt-get", "install", "-y", "php8.3-cli"},
		Exit:    &code,
	}); err != nil {
		t.Fatalf("write finish: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("linhas = %d, esperado 2", len(lines))
	}
	for i, line := range lines {
		var ev journalEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("linha %d inválida: %v", i, err)
		}
		if ev.Time == "" || ev.Action != core.ActionInstall || ev.Step != "install" {
			t.Errorf("evento %d = %+v", i, ev)
		}
	}
}

func TestJournalFileMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := newJournal(path)
	if err != nil {
		t.Fatalf("newJournal: %v", err)
	}
	code := 0
	if err := j.write(journalEvent{
		Phase:   "finish",
		Action:  core.ActionInstall,
		Version: "8.4",
		Step:    "write-sury-source",
		File:    "/etc/apt/sources.list.d/sury-php.list",
		SHA256:  "abc123",
		Exit:    &code,
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ev journalEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("journal inválido: %v", err)
	}
	if ev.File != "/etc/apt/sources.list.d/sury-php.list" || ev.SHA256 != "abc123" {
		t.Errorf("metadados de arquivo = %+v", ev)
	}
	if ev.Argv != nil {
		t.Errorf("passo de arquivo não deveria ter argv: %+v", ev.Argv)
	}
}

func TestJournalRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "journal.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := newJournal(link); err == nil {
		t.Error("journal symlink foi aceito")
	}
}
