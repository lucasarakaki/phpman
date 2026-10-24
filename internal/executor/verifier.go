package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/lucasarakaki/phpman/internal/core"
	"github.com/lucasarakaki/phpman/internal/detect"
)

// OutputRunner runs a read-only command and returns its combined output.
type OutputRunner interface {
	Run(ctx context.Context, argv []string) (string, error)
}

// LocalRunner executes verification commands as the unprivileged user.
type LocalRunner struct{}

func (LocalRunner) Run(ctx context.Context, argv []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = commandEnv()
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Verifier validates a plan step after it runs.
type Verifier interface {
	Verify(ctx context.Context, step core.Step) error
}

// SystemVerifier translates declarative VerifyKind values into real checks.
type SystemVerifier struct {
	Runner OutputRunner
}

func (v SystemVerifier) Verify(ctx context.Context, step core.Step) error {
	if step.Verify == "" {
		return nil
	}
	runner := v.Runner
	if runner == nil {
		runner = LocalRunner{}
	}

	switch step.Verify {
	case core.VerifyPackagesInstalled:
		if len(step.VerifyArgs) == 0 {
			return fmt.Errorf("verificação %s sem pacotes", step.Verify)
		}
		for _, pkg := range step.VerifyArgs {
			out, err := runner.Run(ctx, []string{"dpkg-query", "-W", "-f=${Status}", pkg})
			if err != nil || !strings.Contains(out, "install ok installed") {
				return fmt.Errorf("pacote %s não está instalado: %s", pkg, strings.TrimSpace(out))
			}
		}
		return nil

	case core.VerifyPackagesAbsent:
		if len(step.VerifyArgs) == 0 {
			return fmt.Errorf("verificação %s sem pacotes", step.Verify)
		}
		for _, pkg := range step.VerifyArgs {
			out, err := runner.Run(ctx, []string{"dpkg-query", "-W", "-f=${Status}", pkg})
			if strings.Contains(out, "install ok installed") {
				return fmt.Errorf("pacote %s continua instalado", pkg)
			}
			if err != nil && !strings.Contains(out, "no packages found matching") {
				return fmt.Errorf("não foi possível verificar o pacote %s: %w (%s)", pkg, err, strings.TrimSpace(out))
			}
		}
		return nil

	case core.VerifyBinaryVersion:
		if len(step.VerifyArgs) != 2 {
			return fmt.Errorf("verificação %s exige binário e versão", step.Verify)
		}
		major, minor, err := parseRawVersion(step.VerifyArgs[1])
		if err != nil {
			return err
		}
		out, err := runner.Run(ctx, []string{step.VerifyArgs[0], "-v"})
		if err != nil {
			return fmt.Errorf("não foi possível executar %s -v: %w", step.VerifyArgs[0], err)
		}
		gotMajor, gotMinor, _, _, ok := detect.ParseVersionOutput(out)
		if !ok {
			return fmt.Errorf("saída de %s -v não contém uma versão PHP", step.VerifyArgs[0])
		}
		if gotMajor != major || gotMinor != minor {
			return fmt.Errorf("%s respondeu %d.%d, esperado %s", step.VerifyArgs[0], gotMajor, gotMinor, step.VerifyArgs[1])
		}
		return nil

	case core.VerifyUnitActive:
		if len(step.VerifyArgs) != 1 {
			return fmt.Errorf("verificação %s exige uma unidade", step.Verify)
		}
		out, err := runner.Run(ctx, []string{"systemctl", "is-active", step.VerifyArgs[0]})
		if err != nil || strings.TrimSpace(out) != "active" {
			return fmt.Errorf("unidade %s não está ativa: %s", step.VerifyArgs[0], strings.TrimSpace(out))
		}
		return nil

	case core.VerifyUnitInactive:
		if len(step.VerifyArgs) != 1 {
			return fmt.Errorf("verificação %s exige uma unidade", step.Verify)
		}
		out, err := runner.Run(ctx, []string{"systemctl", "is-active", step.VerifyArgs[0]})
		state := strings.TrimSpace(out)
		switch state {
		case "inactive", "failed", "unknown":
			return nil
		}
		if err != nil {
			return fmt.Errorf("não foi possível verificar a unidade %s: %w", step.VerifyArgs[0], err)
		}
		return fmt.Errorf("unidade %s continua ativa: %s", step.VerifyArgs[0], state)

	case core.VerifyUnitEnabled:
		if len(step.VerifyArgs) != 1 {
			return fmt.Errorf("verificação %s exige uma unidade", step.Verify)
		}
		out, err := runner.Run(ctx, []string{"systemctl", "is-enabled", step.VerifyArgs[0]})
		state := strings.TrimSpace(out)
		switch state {
		case "enabled", "enabled-runtime":
			return nil
		}
		if err != nil {
			return fmt.Errorf("unidade %s não está habilitada: %w (%s)", step.VerifyArgs[0], err, state)
		}
		return fmt.Errorf("unidade %s não está habilitada: %s", step.VerifyArgs[0], state)

	case core.VerifySocketExists:
		if len(step.VerifyArgs) != 1 {
			return fmt.Errorf("verificação %s exige um caminho", step.Verify)
		}
		info, err := os.Stat(step.VerifyArgs[0])
		if err != nil {
			return fmt.Errorf("socket %s não existe: %w", step.VerifyArgs[0], err)
		}
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("%s existe, mas não é um socket", step.VerifyArgs[0])
		}
		return nil

	case core.VerifyAlternativeCurrent:
		if len(step.VerifyArgs) != 2 {
			return fmt.Errorf("verificação %s exige nome e caminho", step.Verify)
		}
		out, err := runner.Run(ctx, []string{"update-alternatives", "--query", step.VerifyArgs[0]})
		if err != nil {
			return fmt.Errorf("não foi possível consultar a alternative %s: %w", step.VerifyArgs[0], err)
		}
		alt, ok := detect.ParseAlternativesQuery(out)
		if !ok || alt.Current != step.VerifyArgs[1] {
			return fmt.Errorf("alternative %s aponta para %q, esperado %q", step.VerifyArgs[0], alt.Current, step.VerifyArgs[1])
		}
		return nil

	case core.VerifyFileContent:
		if step.File == nil {
			return fmt.Errorf("verificação %s exige o conteúdo esperado do passo", step.Verify)
		}
		data, err := os.ReadFile(step.File.Path)
		if err != nil {
			return fmt.Errorf("não foi possível ler %s: %w", step.File.Path, err)
		}
		if string(data) != step.File.Content {
			return fmt.Errorf("conteúdo de %s não confere com o plano", step.File.Path)
		}
		return nil

	case core.VerifyPGPKey:
		if len(step.VerifyArgs) != 2 {
			return fmt.Errorf("verificação %s exige caminho e fingerprint", step.Verify)
		}
		return verifyPGPKey(ctx, runner, step.VerifyArgs[0], step.VerifyArgs[1])
	}

	return fmt.Errorf("verificação desconhecida: %s", step.Verify)
}

func verifyPGPKey(ctx context.Context, runner OutputRunner, path, expected string) error {
	home, err := os.MkdirTemp("", "phpman-gpg-")
	if err != nil {
		return fmt.Errorf("não foi possível criar diretório temporário para o gpg: %w", err)
	}
	defer os.RemoveAll(home)
	_ = os.Chmod(home, 0o700)

	out, err := runner.Run(ctx, []string{
		"/usr/bin/gpg", "--homedir", home, "--batch", "--no-tty", "--no-options",
		"--with-colons", "--with-fingerprint", "--show-keys", path,
	})
	if err != nil {
		return fmt.Errorf("não foi possível inspecionar a chave %s: %w (%s)", path, err, strings.TrimSpace(out))
	}
	want := normalizeFingerprint(expected)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) > 9 && fields[0] == "fpr" && normalizeFingerprint(fields[9]) == want {
			return nil
		}
	}
	return fmt.Errorf("fingerprint da chave %s não confere; esperado %s", path, expected)
}

func normalizeFingerprint(value string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), " ", ""))
}

func parseRawVersion(raw string) (int, int, error) {
	parts := strings.SplitN(raw, ".", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("versão esperada inválida: %q", raw)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("versão esperada inválida: %q", raw)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("versão esperada inválida: %q", raw)
	}
	return major, minor, nil
}
