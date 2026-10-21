package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lucasarakaki/phpman/internal/core"
)

func TestWritePlan(t *testing.T) {
	plan := &core.Plan{
		Schema:    core.PlanSchema,
		Action:    core.ActionInstall,
		Version:   "8.3",
		Summary:   "Instalar PHP 8.3",
		NeedsRoot: true,
		Warnings:  []string{"A versão ativa continuará sendo PHP 8.2."},
		Steps: []core.Step{{
			ID:          "install-packages",
			Description: "Instalar pacotes",
			Argv:        []string{"apt-get", "install", "-y", "php8.3-cli"},
			NeedsRoot:   true,
			Verify:      core.VerifyPackagesInstalled,
			VerifyArgs:  []string{"php8.3-cli"},
		}, {
			ID:          "verify-binary",
			Description: "Confirmar versão",
			Verify:      core.VerifyBinaryVersion,
			VerifyArgs:  []string{"/usr/bin/php8.3", "8.3"},
		}},
		Rollback: []core.Step{{
			ID:          "rollback-remove-packages",
			Description: "Remover pacotes",
			Argv:        []string{"apt-get", "purge", "-y", "php8.3-cli"},
			NeedsRoot:   true,
		}},
	}

	var buf bytes.Buffer
	WritePlan(&buf, plan, false)
	out := buf.String()

	for _, want := range []string{
		"simulação, nada será alterado",
		"Instalar PHP 8.3",
		"requer root: sim",
		"apt-get install -y php8.3-cli",
		"verificação: binary-version /usr/bin/php8.3 8.3",
		"AVISOS",
		"ROLLBACK",
		"apt-get purge -y php8.3-cli",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("saída não contém %q:\n%s", want, out)
		}
	}
}

func TestWritePlanExecution(t *testing.T) {
	plan := &core.Plan{
		Schema:  core.PlanSchema,
		Action:  core.ActionRemove,
		Version: "8.2",
		Summary: "Remover PHP 8.2",
		Steps: []core.Step{{
			ID:          "purge",
			Description: "Remover pacotes",
			Argv:        []string{"apt-get", "purge", "-y", "php8.2-cli"},
			NeedsRoot:   true,
		}},
		Rollback: []core.Step{{
			ID:          "rollback",
			Description: "Reinstalar pacotes",
			Argv:        []string{"apt-get", "install", "-y", "php8.2-cli"},
			NeedsRoot:   true,
		}},
	}

	var buf bytes.Buffer
	WritePlanExecution(&buf, plan, false)
	out := buf.String()
	for _, want := range []string{
		"revise antes de executar",
		"apt-get purge -y php8.2-cli",
		"rollback está planejado",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("saída não contém %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "nada será alterado") {
		t.Errorf("plano de execução ainda se apresenta como simulação:\n%s", out)
	}
}

func TestWritePlanJSON(t *testing.T) {
	plan := &core.Plan{
		Schema:  core.PlanSchema,
		Action:  core.ActionSwitch,
		Version: "8.3",
		Summary: "Trocar para PHP 8.3",
		Steps: []core.Step{{
			ID:          "switch-php",
			Description: "Trocar php",
			Argv:        []string{"update-alternatives", "--set", "php", "/usr/bin/php8.3"},
			NeedsRoot:   true,
			Verify:      core.VerifyAlternativeCurrent,
			VerifyArgs:  []string{"php", "/usr/bin/php8.3"},
		}},
	}

	var buf bytes.Buffer
	if err := WritePlanJSON(&buf, plan); err != nil {
		t.Fatalf("WritePlanJSON: %v", err)
	}
	var got core.Plan
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("JSON inválido: %v", err)
	}
	if got.Schema != core.PlanSchema || got.Action != core.ActionSwitch || len(got.Steps) != 1 {
		t.Fatalf("plano decodificado = %+v", got)
	}
	if got.Steps[0].Argv[3] != "/usr/bin/php8.3" {
		t.Errorf("argv = %+v", got.Steps[0].Argv)
	}
}
