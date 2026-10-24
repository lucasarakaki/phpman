package executor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// writeFileAtomic replaces path with content only after the target directory and
// any existing target file have passed the root-owned/not-writable checks. The
// write goes to a temporary file in the same directory and is renamed into
// place, so apt never observes a partially written source file.
func writeFileAtomic(path, content string, mode os.FileMode) error {
	return writeFileAtomicValidated(path, content, mode, validateRootOwnedDir, validateRootOwnedFile)
}

func writeFileAtomicValidated(
	path, content string,
	mode os.FileMode,
	validateDir func(string) error,
	validateFile func(string) error,
) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("caminho de arquivo inválido: %q", path)
	}
	dir := filepath.Dir(path)
	if err := validateDir(dir); err != nil {
		return err
	}
	if err := validateFile(path); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".phpman-*.tmp")
	if err != nil {
		return fmt.Errorf("não foi possível criar arquivo temporário em %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(mode.Perm()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("não foi possível ajustar o modo de %s: %w", tmpName, err)
	}
	if _, err := io.WriteString(tmp, content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("não foi possível escrever %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("não foi possível sincronizar %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("não foi possível fechar %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("não foi possível substituir %s: %w", path, err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func validateRootOwnedDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("não foi possível inspecionar o diretório %s: %w", dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("o diretório %s não pode ser um symlink", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s não é um diretório", dir)
	}
	if !rootOwnedAndNotWritable(info) {
		return fmt.Errorf("o diretório %s precisa ser root-owned e não gravável por grupo/outros", dir)
	}
	return nil
}

func validateRootOwnedFile(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("não foi possível inspecionar %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("o arquivo %s não pode ser um symlink", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s existe, mas não é um arquivo regular", path)
	}
	if !rootOwnedAndNotWritable(info) {
		return fmt.Errorf("o arquivo %s precisa ser root-owned e não gravável por grupo/outros", path)
	}
	return nil
}
