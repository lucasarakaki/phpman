package executor

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestInvokingUID(t *testing.T) {
	t.Setenv("SUDO_UID", "1000")
	uid, err := invokingUID()
	if err != nil || uid != 1000 {
		t.Fatalf("uid = %d, err = %v", uid, err)
	}
	t.Setenv("SUDO_UID", "")
	if _, err := invokingUID(); err == nil {
		t.Error("SUDO_UID ausente foi aceito")
	}
	t.Setenv("SUDO_UID", "abc")
	if _, err := invokingUID(); err == nil {
		t.Error("SUDO_UID inválido foi aceito")
	}
}

func TestValidateHelperSocket(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, helperSocketName)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	uid := uint32(os.Getuid())
	if err := validateHelperSocket(path, uid); err != nil {
		t.Fatalf("socket válido rejeitado: %v", err)
	}
	if err := validateHelperSocket(path, uid+1); err == nil {
		t.Error("socket de outro uid foi aceito")
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := validateHelperSocket(path, uid); err == nil {
		t.Error("socket gravável por outros foi aceito")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateHelperSocket(path, uid); err == nil {
		t.Error("diretório acessível por outros foi aceito")
	}
}

func TestConnectHelperSocket(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, helperSocketName)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SUDO_UID", strconv.Itoa(os.Getuid()))
	accepted := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
		accepted <- err
	}()

	conn, err := ConnectHelperSocket(path)
	if err != nil {
		t.Fatalf("ConnectHelperSocket: %v", err)
	}
	_ = conn.Close()
	if err := <-accepted; err != nil {
		t.Fatalf("accept: %v", err)
	}
}

func TestPeerUID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	for name, conn := range map[string]net.Conn{"client": client, "server": server} {
		uid, err := peerUID(conn)
		if err != nil {
			t.Fatalf("peerUID(%s): %v", name, err)
		}
		if uid != uint32(os.Getuid()) {
			t.Errorf("peerUID(%s) = %d, esperado %d", name, uid, os.Getuid())
		}
	}
}
