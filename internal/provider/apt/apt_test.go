package apt

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/lucasarakaki/phpman/internal/core"
	"github.com/lucasarakaki/phpman/internal/provider"
)

func TestPlanInstall(t *testing.T) {
	snap := testSnapshot()
	plan, err := New().PlanInstall(snap, "8.3", provider.DefaultOptions())
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}

	if plan.Schema != core.PlanSchema || plan.Action != core.ActionInstall || plan.Version != "8.3" {
		t.Fatalf("plano = %+v", plan)
	}
	if !plan.NeedsRoot {
		t.Error("instalação deveria requerer root")
	}
	if len(plan.Steps) != 4 {
		t.Fatalf("passos = %+v", plan.Steps)
	}

	want := []string{
		"apt-get", "install", "-y", "--no-install-recommends",
		"php8.3-cli", "php8.3-common", "php8.3-fpm", "php8.3-opcache",
	}
	if !reflect.DeepEqual(plan.Steps[0].Argv, want) {
		t.Errorf("argv = %#v, esperado %#v", plan.Steps[0].Argv, want)
	}
	if plan.Steps[1].Argv[0] != "systemctl" || plan.Steps[1].Argv[3] != "php8.3-fpm.service" {
		t.Errorf("passo do FPM = %+v", plan.Steps[1])
	}
	if plan.Steps[2].Verify != core.VerifyBinaryVersion {
		t.Errorf("verificação do binário = %+v", plan.Steps[2])
	}
	if len(plan.Rollback) != 2 {
		t.Errorf("rollback = %+v", plan.Rollback)
	}
	if !hasWarning(plan.Warnings, "versão ativa continuará") {
		t.Errorf("faltou aviso sobre versão ativa: %+v", plan.Warnings)
	}
}

func TestPlanInstallWithoutFPM(t *testing.T) {
	plan, err := New().PlanInstall(testSnapshot(), "8.3", provider.Options{})
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	argv := strings.Join(plan.Steps[0].Argv, " ")
	if strings.Contains(argv, "php8.3-fpm") {
		t.Errorf("plano sem FPM incluiu o pacote do FPM: %s", argv)
	}
	for _, step := range plan.Steps {
		if len(step.Argv) > 0 && step.Argv[0] == "systemctl" {
			t.Errorf("plano sem FPM incluiu systemctl: %+v", step)
		}
	}
}

func TestPlanInstallAlreadyInstalled(t *testing.T) {
	plan, err := New().PlanInstall(testSnapshot(), "8.2", provider.DefaultOptions())
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	if len(plan.Steps) != 0 {
		t.Fatalf("esperava plano vazio, obtive %+v", plan.Steps)
	}
	if !hasWarning(plan.Warnings, "Nenhuma alteração") {
		t.Errorf("faltou aviso de idempotência: %+v", plan.Warnings)
	}
}

func TestPlanInstallUnavailable(t *testing.T) {
	_, err := New().PlanInstall(testSnapshot(), "8.4", provider.DefaultOptions())
	if !errors.Is(err, provider.ErrVersionUnavailable) {
		t.Fatalf("err = %v, esperado ErrVersionUnavailable", err)
	}
}

func TestPlanInstallWarnsThirdParty(t *testing.T) {
	snap := testSnapshot()
	snap.Available[0].Origin = "terceiros"
	snap.Available[0].Source = "packages.sury.org/php"
	plan, err := New().PlanInstall(snap, "8.3", provider.DefaultOptions())
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	if !hasWarning(plan.Warnings, "packages.sury.org/php") {
		t.Errorf("faltou aviso de terceiros: %+v", plan.Warnings)
	}
}

func TestPlanRemoveBlocksActiveVersion(t *testing.T) {
	snap := testSnapshot()
	_, err := New().PlanRemove(snap, "8.2")
	if !errors.Is(err, provider.ErrActiveVersion) {
		t.Fatalf("err = %v, esperado ErrActiveVersion", err)
	}
}

func TestPlanRemove(t *testing.T) {
	snap := testSnapshot()
	snap.ActiveVersion = "8.3"
	snap.Versions = append(snap.Versions, core.Version{Raw: "8.3", Major: 8, Minor: 3})

	plan, err := New().PlanRemove(snap, "8.2")
	if err != nil {
		t.Fatalf("PlanRemove: %v", err)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("passos = %+v", plan.Steps)
	}
	want := []string{
		"apt-get", "purge", "-y",
		"libapache2-mod-php8.2", "php8.2-cli", "php8.2-common", "php8.2-fpm", "php8.2-opcache",
	}
	if !reflect.DeepEqual(plan.Steps[1].Argv, want) {
		t.Errorf("argv = %#v, esperado %#v", plan.Steps[1].Argv, want)
	}
	if !hasWarning(plan.Warnings, "/etc/nginx/sites-enabled/app.conf:12") {
		t.Errorf("faltou aviso de vhost: %+v", plan.Warnings)
	}
	if len(plan.Rollback) != 1 {
		t.Errorf("rollback = %+v", plan.Rollback)
	}
}

func TestPlanRemoveUnknownVersion(t *testing.T) {
	_, err := New().PlanRemove(testSnapshot(), "8.4")
	if !errors.Is(err, provider.ErrVersionNotInstalled) {
		t.Fatalf("err = %v, esperado ErrVersionNotInstalled", err)
	}
}

func TestPlanRemoveNoPackages(t *testing.T) {
	snap := testSnapshot()
	snap.ActiveVersion = "8.3"
	snap.Versions = append(snap.Versions, core.Version{Raw: "8.3", Major: 8, Minor: 3})
	snap.Packages = nil
	snap.Versions[0].Packages = nil

	_, err := New().PlanRemove(snap, "8.2")
	if !errors.Is(err, provider.ErrNoPackages) {
		t.Fatalf("err = %v, esperado ErrNoPackages", err)
	}
}

func TestPlanSwitch(t *testing.T) {
	snap := testSnapshot()
	snap.Versions = append(snap.Versions, core.Version{Raw: "8.3", Major: 8, Minor: 3})

	plan, err := New().PlanSwitch(snap, "8.3")
	if err != nil {
		t.Fatalf("PlanSwitch: %v", err)
	}
	if len(plan.Steps) != 6 {
		t.Fatalf("passos = %+v", plan.Steps)
	}
	wantPHP := []string{"update-alternatives", "--set", "php", "/usr/bin/php8.3"}
	if !reflect.DeepEqual(plan.Steps[0].Argv, wantPHP) {
		t.Errorf("argv = %#v, esperado %#v", plan.Steps[0].Argv, wantPHP)
	}
	if plan.Steps[5].Verify != core.VerifyBinaryVersion {
		t.Errorf("último passo = %+v", plan.Steps[5])
	}
	if len(plan.Rollback) != 5 {
		t.Errorf("rollback = %+v", plan.Rollback)
	}
	if !hasWarning(plan.Warnings, "não são reiniciados ou reescritos") {
		t.Errorf("faltou aviso sobre serviços: %+v", plan.Warnings)
	}
}

func TestPlanSwitchAlreadyActive(t *testing.T) {
	plan, err := New().PlanSwitch(testSnapshot(), "8.2")
	if err != nil {
		t.Fatalf("PlanSwitch: %v", err)
	}
	if len(plan.Steps) != 0 {
		t.Fatalf("esperava plano vazio, obtive %+v", plan.Steps)
	}
}

func TestPlanSwitchWithoutCandidates(t *testing.T) {
	snap := testSnapshot()
	snap.Versions = append(snap.Versions, core.Version{Raw: "8.3", Major: 8, Minor: 3})
	snap.Alternatives = []core.Alternative{{Name: "php", Current: "/usr/bin/php8.2"}}

	plan, err := New().PlanSwitch(snap, "8.3")
	if err != nil {
		t.Fatalf("PlanSwitch: %v", err)
	}
	if len(plan.Steps) != 0 {
		t.Fatalf("esperava plano vazio, obtive %+v", plan.Steps)
	}
	if !hasWarning(plan.Warnings, "Não foi possível localizar") {
		t.Errorf("faltou aviso de candidato ausente: %+v", plan.Warnings)
	}
}

func TestPlanDeterministic(t *testing.T) {
	snap := testSnapshot()
	first, err := New().PlanInstall(snap, "8.3", provider.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	second, err := New().PlanInstall(snap, "8.3", provider.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Error("planos deveriam ser determinísticos")
	}
}

func TestPlanArgvHasNoShell(t *testing.T) {
	snap := testSnapshot()
	plans := []*core.Plan{}
	install, err := New().PlanInstall(snap, "8.3", provider.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	plans = append(plans, install)

	snap.ActiveVersion = "8.3"
	snap.Versions = append(snap.Versions, core.Version{Raw: "8.3", Major: 8, Minor: 3})
	remove, err := New().PlanRemove(snap, "8.2")
	if err != nil {
		t.Fatal(err)
	}
	plans = append(plans, remove)

	switchPlan, err := New().PlanSwitch(snap, "8.2")
	if err != nil {
		t.Fatal(err)
	}
	plans = append(plans, switchPlan)

	for _, plan := range plans {
		for _, step := range append(append([]core.Step{}, plan.Steps...), plan.Rollback...) {
			if len(step.Argv) == 0 {
				continue
			}
			switch step.Argv[0] {
			case "sh", "bash", "zsh", "fish":
				t.Errorf("passo usa shell: %+v", step)
			}
			for _, arg := range step.Argv {
				if strings.ContainsAny(arg, "|&;<>$`\n") {
					t.Errorf("argumento com metacaractere de shell: %q", arg)
				}
			}
		}
	}
}

func TestPlanInvalidVersion(t *testing.T) {
	for _, raw := range []string{"8", "8.3.1", "php8.3", ""} {
		if _, err := New().PlanInstall(testSnapshot(), raw, provider.DefaultOptions()); !errors.Is(err, provider.ErrInvalidVersion) {
			t.Errorf("raw=%q: err = %v, esperado ErrInvalidVersion", raw, err)
		}
	}
}

func TestPlanUnsupportedDistro(t *testing.T) {
	snap := testSnapshot()
	snap.Distro.Family = core.FamilyFedora
	_, err := New().PlanInstall(snap, "8.3", provider.DefaultOptions())
	if !errors.Is(err, provider.ErrUnsupportedDistro) {
		t.Fatalf("err = %v, esperado ErrUnsupportedDistro", err)
	}
}

func hasWarning(warnings []string, fragment string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, fragment) {
			return true
		}
	}
	return false
}

func testSnapshot() *core.Snapshot {
	return &core.Snapshot{
		Schema:        core.Schema,
		Distro:        core.Distro{ID: "debian", Family: core.FamilyDebian, PackageManager: "apt"},
		ActiveVersion: "8.2",
		PathVersion:   "8.2",
		PathBinary:    "/usr/bin/php8.2",
		Versions: []core.Version{{
			Raw:    "8.2",
			Major:  8,
			Minor:  2,
			Active: true,
			InPath: true,
			SAPIs: []core.SAPI{{
				Kind:   core.SAPIFPM,
				Unit:   "php8.2-fpm.service",
				Socket: "/run/php/php8.2-fpm.sock",
			}},
		}},
		Available: []core.AvailableVersion{{
			Raw:       "8.3",
			Package:   "php8.3-cli",
			Candidate: "8.3.27-1",
			Origin:    "distro",
			Source:    "deb.debian.org/debian",
		}},
		Packages: []core.Package{
			{Name: "libapache2-mod-php8.2", Manager: "dpkg"},
			{Name: "php8.2-cli", Manager: "dpkg"},
			{Name: "php8.2-common", Manager: "dpkg"},
			{Name: "php8.2-fpm", Manager: "dpkg"},
			{Name: "php8.2-opcache", Manager: "dpkg"},
			{Name: "php8.3-cli", Manager: "dpkg"},
		},
		Sockets: []core.Socket{{
			Path:    "/run/php/php8.2-fpm.sock",
			Version: "8.2",
			Exists:  true,
		}},
		Vhosts: []core.Vhost{{
			File:   "/etc/nginx/sites-enabled/app.conf",
			Line:   12,
			Kind:   "fpm-socket",
			Target: "/run/php/php8.2-fpm.sock",
		}},
		Alternatives: []core.Alternative{
			{
				Name:    "php",
				Current: "/usr/bin/php8.2",
				Candidates: []core.AlternativeCandidate{
					{Path: "/usr/bin/php8.2", Priority: 82},
					{Path: "/usr/bin/php8.3", Priority: 83},
				},
			},
			{
				Name:    "phpize",
				Current: "/usr/bin/phpize8.2",
				Candidates: []core.AlternativeCandidate{
					{Path: "/usr/bin/phpize8.2", Priority: 82},
					{Path: "/usr/bin/phpize8.3", Priority: 83},
				},
			},
			{
				Name:    "php-config",
				Current: "/usr/bin/php-config8.2",
				Candidates: []core.AlternativeCandidate{
					{Path: "/usr/bin/php-config8.2", Priority: 82},
					{Path: "/usr/bin/php-config8.3", Priority: 83},
				},
			},
			{
				Name:    "phpdbg",
				Current: "/usr/bin/phpdbg8.2",
				Candidates: []core.AlternativeCandidate{
					{Path: "/usr/bin/phpdbg8.2", Priority: 82},
					{Path: "/usr/bin/phpdbg8.3", Priority: 83},
				},
			},
			{
				Name:    "phar",
				Current: "/usr/bin/phar8.2",
				Candidates: []core.AlternativeCandidate{
					{Path: "/usr/bin/phar8.2", Priority: 82},
					{Path: "/usr/bin/phar8.3", Priority: 83},
				},
			},
		},
	}
}
