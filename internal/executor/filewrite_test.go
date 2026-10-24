package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileAtomicValidated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sury.list")
	content := "deb [signed-by=" + suryKeyring + "] https://packages.sury.org/php/ bookworm main\n"
	noop := func(string) error { return nil }

	if err := writeFileAtomicValidated(path, content, 0o644, noop, noop); err != nil {
		t.Fatalf("writeFileAtomicValidated: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Errorf("conteúdo = %q, esperado %q", data, content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("modo = %#o, esperado 0644", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".phpman-") {
			t.Errorf("arquivo temporário não removido: %s", entry.Name())
		}
	}
}

func TestValidateRootOwnedFileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := validateRootOwnedFile(link); err == nil {
		t.Error("symlink foi aceito como destino de escrita")
	}
}

func TestWriteFileAtomicValidatedRejectsRelativePath(t *testing.T) {
	noop := func(string) error { return nil }
	if err := writeFileAtomicValidated("relative.list", "x", 0o644, noop, noop); err == nil {
		t.Error("caminho relativo foi aceito")
	}
}
