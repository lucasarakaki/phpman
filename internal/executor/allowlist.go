package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/lucasarakaki/phpman/internal/core"
)

const (
	suryKeyring    = "/usr/share/keyrings/deb.sury.org-php.gpg"
	surySourceFile = "/etc/apt/sources.list.d/sury-php.list"
	suryKeyURL     = "https://packages.sury.org/php/apt.gpg"
)

var (
	planVersionRe   = regexp.MustCompile(`^\d+\.\d+$`)
	phpPackageRe    = regexp.MustCompile(`^(?:php|libphp|libapache2-mod-php)\d+\.\d+(?:-[a-z0-9+.-]+)?$`)
	phpFPMUnitRe    = regexp.MustCompile(`^php(?:[0-9]+\.[0-9]+)?-fpm\.service$`)
	surySourceRe    = regexp.MustCompile(`^deb \[signed-by=/usr/share/keyrings/deb\.sury\.org-php\.gpg\] https://packages\.sury\.org/php/ [a-z0-9][a-z0-9-]{0,31} main\n$`)
	alternativeName = map[string]bool{
		"php":        true,
		"phpize":     true,
		"php-config": true,
		"phpdbg":     true,
		"phar":       true,
	}
	allowedCommandPaths = map[string][]string{
		"apt-get":             {"/usr/bin/apt-get"},
		"systemctl":           {"/usr/bin/systemctl", "/bin/systemctl"},
		"update-alternatives": {"/usr/sbin/update-alternatives", "/usr/bin/update-alternatives"},
		"curl":                {"/usr/bin/curl"},
		"chmod":               {"/usr/bin/chmod", "/bin/chmod"},
	}
)

// validateArgv enforces the command allowlist. It checks the exact shape of
// every command; it does not resolve binaries (see resolveArgv).
func validateArgv(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("argv vazio")
	}
	if len(argv) > 64 {
		return fmt.Errorf("argv com argumentos demais")
	}
	for _, arg := range argv {
		if arg == "" {
			return fmt.Errorf("argumento vazio em argv")
		}
		if len(arg) > 256 {
			return fmt.Errorf("argumento longo demais em argv")
		}
		if unsafeArg(arg) {
			return fmt.Errorf("argumento inseguro em argv: %q", arg)
		}
	}

	switch argv[0] {
	case "apt-get":
		return validateAptGet(argv)
	case "systemctl":
		return validateSystemctl(argv)
	case "update-alternatives":
		return validateUpdateAlternatives(argv)
	case "curl":
		return validateCurl(argv)
	case "chmod":
		return validateChmod(argv)
	default:
		return fmt.Errorf("comando fora da allowlist: %q", argv[0])
	}
}

func unsafeArg(arg string) bool {
	if strings.ContainsAny(arg, "|&;<>`$\\") {
		return true
	}
	for _, r := range arg {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

func validateAptGet(argv []string) error {
	if len(argv) == 2 && argv[1] == "update" {
		return nil
	}
	if isSuryBootstrapInstall(argv) {
		return nil
	}
	if len(argv) < 4 {
		return fmt.Errorf("apt-get exige subcomando, -y e ao menos um pacote")
	}
	if argv[1] != "install" && argv[1] != "purge" {
		return fmt.Errorf("subcomando apt-get fora da allowlist: %q", argv[1])
	}
	if argv[2] != "-y" {
		return fmt.Errorf("apt-get precisa ser executado com -y")
	}
	start := 3
	if argv[1] == "install" && start < len(argv) && argv[start] == "--no-install-recommends" {
		start++
	}
	if start >= len(argv) {
		return fmt.Errorf("apt-get sem pacotes")
	}
	for _, pkg := range argv[start:] {
		if !phpPackageRe.MatchString(pkg) {
			return fmt.Errorf("pacote fora do escopo do phpman: %q", pkg)
		}
	}
	return nil
}

func isSuryBootstrapInstall(argv []string) bool {
	want := []string{
		"apt-get", "install", "-y", "--no-install-recommends",
		"ca-certificates", "curl", "gnupg",
	}
	return sameArgv(argv, want)
}

func validateCurl(argv []string) error {
	want := []string{
		"curl", "--disable", "--fail", "--silent", "--show-error",
		"--location", "--proto", "=https", "--proto-redir", "=https", "--tlsv1.2",
		"--connect-timeout", "15", "--max-time", "60",
		"--output", suryKeyring, suryKeyURL,
	}
	if !sameArgv(argv, want) {
		return fmt.Errorf("curl só pode baixar a chave Sury/ondrej para o keyring fixo")
	}
	return nil
}

func validateChmod(argv []string) error {
	if len(argv) != 3 || argv[1] != "0644" || argv[2] != suryKeyring {
		return fmt.Errorf("chmod só pode ajustar o keyring Sury/ondrej para 0644")
	}
	return nil
}

func validateFileWrite(file *core.FileWrite) error {
	if file == nil {
		return fmt.Errorf("escrita de arquivo ausente")
	}
	if file.Path != surySourceFile {
		return fmt.Errorf("arquivo fora da allowlist: %q", file.Path)
	}
	if file.Mode != 0o644 {
		return fmt.Errorf("modo de arquivo fora da allowlist: %#o", file.Mode)
	}
	if !surySourceRe.MatchString(file.Content) {
		return fmt.Errorf("conteúdo de repositório inválido para %s", file.Path)
	}
	return nil
}

func validateSystemctl(argv []string) error {
	if len(argv) < 3 {
		return fmt.Errorf("systemctl exige uma ação e uma unidade")
	}
	var unit string
	switch argv[1] {
	case "enable", "disable":
		switch {
		case len(argv) == 3:
			unit = argv[2]
		case len(argv) == 4 && argv[2] == "--now":
			unit = argv[3]
		default:
			return fmt.Errorf("forma de systemctl %s não permitida", argv[1])
		}
	case "start", "stop", "restart":
		if len(argv) != 3 {
			return fmt.Errorf("systemctl %s exige exatamente uma unidade", argv[1])
		}
		unit = argv[2]
	default:
		return fmt.Errorf("ação systemctl fora da allowlist: %q", argv[1])
	}
	if !phpFPMUnitRe.MatchString(unit) {
		return fmt.Errorf("unidade fora do escopo do phpman: %q", unit)
	}
	return nil
}

func validateUpdateAlternatives(argv []string) error {
	if len(argv) != 4 || argv[1] != "--set" {
		return fmt.Errorf("update-alternatives só pode ser usado como --set <nome> <caminho>")
	}
	name := argv[2]
	if !alternativeName[name] {
		return fmt.Errorf("alternative fora da allowlist: %q", name)
	}
	path := argv[3]
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("caminho de alternative inválido: %q", path)
	}
	dir := filepath.Dir(path)
	if dir != "/usr/bin" && dir != "/usr/local/bin" {
		return fmt.Errorf("diretório de alternative fora da allowlist: %q", dir)
	}
	base := filepath.Base(path)
	re := regexp.MustCompile(`^` + regexp.QuoteMeta(name) + `[0-9]+\.[0-9]+$`)
	if !re.MatchString(base) {
		return fmt.Errorf("binário de alternative inválido para %s: %q", name, base)
	}
	return nil
}

// resolveArgv validates an argv and replaces argv[0] with a trusted absolute
// path. This prevents PATH hijacking when the helper runs as root.
func resolveArgv(argv []string) ([]string, error) {
	if err := validateArgv(argv); err != nil {
		return nil, err
	}
	path, err := resolveCommand(argv[0])
	if err != nil {
		return nil, err
	}
	resolved := make([]string, len(argv))
	copy(resolved, argv)
	resolved[0] = path
	return resolved, nil
}

func resolveCommand(name string) (string, error) {
	candidates, ok := allowedCommandPaths[name]
	if !ok {
		return "", fmt.Errorf("comando fora da allowlist: %q", name)
	}
	for _, candidate := range candidates {
		if trustedExecutable(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("comando permitido não encontrado ou não confiável: %s", name)
}

// validatePlan checks the plan envelope and returns its executable steps by
// ID. Verification-only steps are accepted but cannot be executed by the
// helper. Steps and rollback steps are validated independently because the
// helper executes them in separate phases.
func validatePlan(plan *core.Plan) (map[string]core.Step, map[string]core.Step, error) {
	if plan == nil {
		return nil, nil, fmt.Errorf("plano ausente")
	}
	if plan.Schema != core.PlanSchema {
		return nil, nil, fmt.Errorf("schema de plano não suportado: %d", plan.Schema)
	}
	switch plan.Action {
	case core.ActionInstall, core.ActionRemove, core.ActionSwitch:
	default:
		return nil, nil, fmt.Errorf("ação de plano desconhecida: %q", plan.Action)
	}
	if !planVersionRe.MatchString(plan.Version) {
		return nil, nil, fmt.Errorf("versão de plano inválida: %q", plan.Version)
	}
	if len(plan.Steps) > 64 || len(plan.Rollback) > 64 {
		return nil, nil, fmt.Errorf("plano com passos demais")
	}

	seen := make(map[string]bool, len(plan.Steps)+len(plan.Rollback))
	index := func(steps []core.Step, phase string) (map[string]core.Step, error) {
		indexed := make(map[string]core.Step, len(steps))
		for _, step := range steps {
			if step.ID == "" {
				return nil, fmt.Errorf("passo sem id em %s", phase)
			}
			if seen[step.ID] {
				return nil, fmt.Errorf("id de passo duplicado: %q", step.ID)
			}
			seen[step.ID] = true
			hasCommand := len(step.Argv) > 0
			hasFile := step.File != nil
			if hasCommand && hasFile {
				return nil, fmt.Errorf("passo %q tem comando e escrita de arquivo ao mesmo tempo", step.ID)
			}
			if hasCommand || hasFile {
				if !step.NeedsRoot {
					return nil, fmt.Errorf("helper só executa passos que exigem root: %q", step.ID)
				}
				if hasCommand {
					if err := validateArgv(step.Argv); err != nil {
						return nil, fmt.Errorf("passo %q: %w", step.ID, err)
					}
				}
				if hasFile {
					if err := validateFileWrite(step.File); err != nil {
						return nil, fmt.Errorf("passo %q: %w", step.ID, err)
					}
				}
			} else if step.Verify == "" {
				return nil, fmt.Errorf("passo %q não tem comando nem verificação", step.ID)
			}
			indexed[step.ID] = step
		}
		return indexed, nil
	}

	steps, err := index(plan.Steps, "passos")
	if err != nil {
		return nil, nil, err
	}
	rollback, err := index(plan.Rollback, "rollback")
	if err != nil {
		return nil, nil, err
	}
	return steps, rollback, nil
}

func sameArgv(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func trustedExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return false
	}
	return rootOwnedAndNotWritable(info)
}
