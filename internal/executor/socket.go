package executor

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

const helperSocketName = "helper.sock"

// ConnectHelperSocket is called by the root helper. It validates that the
// socket belongs to the user who invoked sudo, then connects to it.
func ConnectHelperSocket(path string) (net.Conn, error) {
	invoker, err := invokingUID()
	if err != nil {
		return nil, err
	}
	if err := validateHelperSocket(path, invoker); err != nil {
		return nil, err
	}

	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("não foi possível conectar ao cliente: %w", err)
	}
	peer, err := peerUID(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("não foi possível identificar o cliente: %w", err)
	}
	if peer != invoker {
		_ = conn.Close()
		return nil, fmt.Errorf("cliente inesperado no socket: uid %d, esperado %d", peer, invoker)
	}
	return conn, nil
}

func invokingUID() (uint32, error) {
	raw := os.Getenv("SUDO_UID")
	if raw == "" {
		return 0, fmt.Errorf("SUDO_UID ausente; o helper só pode ser iniciado por sudo")
	}
	uid, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("SUDO_UID inválido: %q", raw)
	}
	return uint32(uid), nil
}

func validateHelperSocket(path string, uid uint32) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("caminho de socket inválido: %q", path)
	}
	if filepath.Base(path) != helperSocketName {
		return fmt.Errorf("nome de socket inválido: %q", filepath.Base(path))
	}

	dirInfo, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("não foi possível inspecionar o diretório do socket: %w", err)
	}
	dirUID, ok := fileOwnerUID(dirInfo)
	if !ok || !dirInfo.IsDir() || dirUID != uid || dirInfo.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("diretório do socket não pertence ao usuário que invocou sudo ou é acessível por outros: %s", filepath.Dir(path))
	}

	sockInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("não foi possível inspecionar o socket: %w", err)
	}
	sockUID, ok := fileOwnerUID(sockInfo)
	if !ok || sockInfo.Mode()&os.ModeSocket == 0 || sockUID != uid || sockInfo.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("socket não pertence ao usuário que invocou sudo ou é acessível por outros: %s", path)
	}
	return nil
}

func fileOwnerUID(info os.FileInfo) (uint32, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Uid, true
}

func peerUID(conn net.Conn) (uint32, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("conexão não é um socket Unix")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var uid uint32
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		cred, err := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		if err != nil {
			sockErr = err
			return
		}
		uid = cred.Uid
	}); err != nil {
		return 0, err
	}
	if sockErr != nil {
		return 0, sockErr
	}
	return uid, nil
}
