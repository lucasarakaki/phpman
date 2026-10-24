package executor

import (
	"os"
	"strings"
	"testing"

	"github.com/lucasarakaki/phpman/internal/core"
)

func TestValidateArgv(t *testing.T) {
	valid := [][]string{
		{"apt-get", "install", "-y", "--no-install-recommends", "php8.3-cli", "php8.3-common"},
		{"apt-get", "install", "-y", "php8.3-fpm"},
		{"apt-get", "purge", "-y", "php8.2-cli", "libapache2-mod-php8.2"},
		{"systemctl", "enable", "--now", "php8.3-fpm.service"},
		{"systemctl", "disable", "php8.2-fpm.service"},
		{"systemctl", "start", "php-fpm.service"},
		{"systemctl", "restart", "php8.3-fpm.service"},
		{"update-alternatives", "--set", "php", "/usr/bin/php8.3"},
		{"update-alternatives", "--set", "php-config", "/usr/local/bin/php-config8.2"},
	}
	for _, argv := range valid {
		if err := validateArgv(argv); err != nil {
			t.Errorf("argv válido rejeitado %q: %v", strings.Join(argv, " "), err)
		}
	}

	invalid := [][]string{
		{},
		{"sh", "-c", "apt-get install php8.3"},
		{"bash", "-lc", "true"},
		{"apt-get", "update"},
		{"apt-get", "install", "php8.3-cli"},
		{"apt-get", "install", "-y", "--no-install-recommends", "nginx"},
		{"apt-get", "install", "-y", "php"},
		{"apt-get", "install", "-y", "php8.3-cli", "--allow-unauthenticated"},
		{"apt-get", "purge", "-y", "php8.2-cli;rm"},
		{"apt-get", "install", "-y", "PHP8.3-cli"},
		{"systemctl", "restart", "nginx.service"},
		{"systemctl", "restart", "php8.3-fpm.service;reboot"},
		{"systemctl", "enable", "--now", "php8.3-fpm.service", "extra"},
		{"systemctl", "mask", "php8.3-fpm.service"},
		{"update-alternatives", "--set", "java", "/usr/bin/java8.2"},
		{"update-alternatives", "--set", "php", "/tmp/php8.3"},
		{"update-alternatives", "--set", "php", "/usr/bin/php"},
		{"update-alternatives", "--set", "php", "/usr/bin/../tmp/php8.3"},
		{"update-alternatives", "--install", "php", "/usr/bin/php8.3", "/usr/bin/php8.3", "10"},
		{"/usr/bin/apt-get", "install", "-y", "php8.3-cli"},
		{"apt-get", "install", "-y", "php8.3-cli\nrm"},
	}
	for _, argv := range invalid {
		if err := validateArgv(argv); err == nil {
			t.Errorf("argv inválido aceito: %q", strings.Join(argv, " "))
		}
	}
}

func TestValidatePlan(t *testing.T) {
	valid := &core.Plan{
		Schema:  core.PlanSchema,
		Action:  core.ActionInstall,
		Version: "8.3",
		Steps: []core.Step{
			{
				ID:        "install",
				Argv:      []string{"apt-get", "install", "-y", "php8.3-cli"},
				NeedsRoot: true,
				Verify:    core.VerifyPackagesInstalled,
			},
			{
				ID:     "verify",
				Verify: core.VerifyBinaryVersion,
			},
		},
		Rollback: []core.Step{
			{
				ID:        "rollback-remove",
				Argv:      []string{"apt-get", "purge", "-y", "php8.3-cli"},
				NeedsRoot: true,
				Verify:    core.VerifyPackagesAbsent,
			},
			{
				ID:     "rollback-verify",
				Verify: core.VerifyPackagesAbsent,
			},
		},
	}
	steps, rollback, err := validatePlan(valid)
	if err != nil {
		t.Fatalf("plano válido rejeitado: %v", err)
	}
	if _, ok := steps["install"]; !ok {
		t.Error("passo install não foi indexado")
	}
	if _, ok := rollback["rollback-remove"]; !ok {
		t.Error("passo rollback-remove não foi indexado")
	}

	cases := []struct {
		name string
		plan *core.Plan
	}{
		{"nil", nil},
		{"schema", &core.Plan{Schema: 99, Action: core.ActionInstall, Version: "8.3"}},
		{"action", &core.Plan{Schema: core.PlanSchema, Action: "upgrade", Version: "8.3"}},
		{"version", &core.Plan{Schema: core.PlanSchema, Action: core.ActionInstall, Version: "8"}},
		{
			"duplicate",
			&core.Plan{Schema: core.PlanSchema, Action: core.ActionInstall, Version: "8.3", Steps: []core.Step{
				{ID: "x", Argv: []string{"apt-get", "install", "-y", "php8.3-cli"}, NeedsRoot: true},
				{ID: "x", Argv: []string{"apt-get", "install", "-y", "php8.3-fpm"}, NeedsRoot: true},
			}},
		},
		{
			"non-root",
			&core.Plan{Schema: core.PlanSchema, Action: core.ActionInstall, Version: "8.3", Steps: []core.Step{
				{ID: "x", Argv: []string{"apt-get", "install", "-y", "php8.3-cli"}},
			}},
		},
		{
			"unallowed",
			&core.Plan{Schema: core.PlanSchema, Action: core.ActionInstall, Version: "8.3", Steps: []core.Step{
				{ID: "x", Argv: []string{"sh", "-c", "true"}, NeedsRoot: true},
			}},
		},
		{
			"empty step",
			&core.Plan{Schema: core.PlanSchema, Action: core.ActionInstall, Version: "8.3", Steps: []core.Step{
				{ID: "x"},
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := validatePlan(tc.plan); err == nil {
				t.Error("plano inválido aceito")
			}
		})
	}
}

func TestValidateHelperBinaryRejectsUserOwned(t *testing.T) {
	path := t.TempDir() + "/phpman"
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateHelperBinary(path); err == nil {
		t.Error("binário do usuário foi aceito para sudo")
	}
}
