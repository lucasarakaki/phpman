package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// rootOwnedAndNotWritable accepts files/directories owned by root that cannot
// be modified by group or other users. The owner (root) may still write.
func rootOwnedAndNotWritable(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != 0 {
		return false
	}
	return info.Mode().Perm()&0o022 == 0
}

// validateHelperBinary refuses to launch a user-controlled executable through
// sudo. Running ./bin/phpman with sudo would otherwise let the invoking user
// modify the binary that root is about to execute.
func validateHelperBinary(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("não foi possível inspecionar o executável: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("o helper não pode ser executado através de symlink; instale o binário com sudo make install")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("executável do helper inválido: %s", path)
	}
	if !rootOwnedAndNotWritable(info) {
		return fmt.Errorf("o helper precisa ser um binário root-owned e não gravável por outros usuários; use sudo make install")
	}

	dir := filepath.Dir(path)
	dirInfo, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("não foi possível inspecionar o diretório do helper: %w", err)
	}
	if !dirInfo.IsDir() || !rootOwnedAndNotWritable(dirInfo) {
		return fmt.Errorf("o diretório do helper precisa ser root-owned e não gravável por outros usuários: %s", dir)
	}
	return nil
}
