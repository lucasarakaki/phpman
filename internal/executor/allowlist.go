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

var (
	planVersionRe   = regexp.MustCompile(`^\d+\.\d+$`)
	phpPackageRe    = regexp.MustCompile(`^(?:php|libphp|libapache2-mod-php)\d+\.\d+(?:-[a-z0-9+.-]+)?$`)
	phpFPMUnitRe    = regexp.MustCompile(`^php(?:[0-9]+\.[0-9]+)?-fpm\.service$`)
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
			if len(step.Argv) > 0 {
				if !step.NeedsRoot {
					return nil, fmt.Errorf("helper só executa passos que exigem root: %q", step.ID)
				}
				if err := validateArgv(step.Argv); err != nil {
					return nil, fmt.Errorf("passo %q: %w", step.ID, err)
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
