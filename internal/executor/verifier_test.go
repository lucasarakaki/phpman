package executor

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/lucasarakaki/phpman/internal/core"
)

type fakeOutput struct {
	outputs map[string]string
	errs    map[string]error
}

func (f fakeOutput) Run(_ context.Context, argv []string) (string, error) {
	key := strings.Join(argv, " ")
	return f.outputs[key], f.errs[key]
}

func TestSystemVerifierPackages(t *testing.T) {
	runner := fakeOutput{outputs: map[string]string{
		"dpkg-query -W -f=${Status} php8.3-cli": "install ok installed",
	}}
	v := SystemVerifier{Runner: runner}
	if err := v.Verify(context.Background(), core.Step{
		Verify:     core.VerifyPackagesInstalled,
		VerifyArgs: []string{"php8.3-cli"},
	}); err != nil {
		t.Fatalf("pacote instalado: %v", err)
	}
	if err := v.Verify(context.Background(), core.Step{
		Verify:     core.VerifyPackagesInstalled,
		VerifyArgs: []string{"php8.3-fpm"},
	}); err == nil {
		t.Error("pacote ausente foi aceito")
	}

	installed := fakeOutput{outputs: map[string]string{
		"dpkg-query -W -f=${Status} php8.2-cli": "install ok installed",
	}}
	if err := (SystemVerifier{Runner: installed}).Verify(context.Background(), core.Step{
		Verify:     core.VerifyPackagesAbsent,
		VerifyArgs: []string{"php8.2-cli"},
	}); err == nil {
		t.Error("pacote instalado foi aceito como ausente")
	}

	absent := fakeOutput{
		outputs: map[string]string{
			"dpkg-query -W -f=${Status} php8.2-cli": "dpkg-query: no packages found matching php8.2-cli",
		},
		errs: map[string]error{
			"dpkg-query -W -f=${Status} php8.2-cli": errors.New("exit status 1"),
		},
	}
	if err := (SystemVerifier{Runner: absent}).Verify(context.Background(), core.Step{
		Verify:     core.VerifyPackagesAbsent,
		VerifyArgs: []string{"php8.2-cli"},
	}); err != nil {
		t.Errorf("pacote ausente: %v", err)
	}
}

func TestSystemVerifierBinaryVersion(t *testing.T) {
	runner := fakeOutput{outputs: map[string]string{
		"/usr/bin/php8.3 -v": "PHP 8.3.4 (cli) (built: Jan 1 2026)\nCopyright",
	}}
	v := SystemVerifier{Runner: runner}
	step := core.Step{Verify: core.VerifyBinaryVersion, VerifyArgs: []string{"/usr/bin/php8.3", "8.3"}}
	if err := v.Verify(context.Background(), step); err != nil {
		t.Fatalf("versão correta: %v", err)
	}
	step.VerifyArgs[1] = "8.2"
	if err := v.Verify(context.Background(), step); err == nil {
		t.Error("versão divergente foi aceita")
	}
}

func TestSystemVerifierUnits(t *testing.T) {
	active := SystemVerifier{Runner: fakeOutput{outputs: map[string]string{
		"systemctl is-active php8.3-fpm.service": "active\n",
	}}}
	if err := active.Verify(context.Background(), core.Step{
		Verify:     core.VerifyUnitActive,
		VerifyArgs: []string{"php8.3-fpm.service"},
	}); err != nil {
		t.Fatalf("unidade ativa: %v", err)
	}

	inactive := SystemVerifier{Runner: fakeOutput{
		outputs: map[string]string{"systemctl is-active php8.3-fpm.service": "inactive\n"},
		errs:    map[string]error{"systemctl is-active php8.3-fpm.service": errors.New("exit status 3")},
	}}
	if err := inactive.Verify(context.Background(), core.Step{
		Verify:     core.VerifyUnitInactive,
		VerifyArgs: []string{"php8.3-fpm.service"},
	}); err != nil {
		t.Fatalf("unidade inativa: %v", err)
	}
	if err := inactive.Verify(context.Background(), core.Step{
		Verify:     core.VerifyUnitActive,
		VerifyArgs: []string{"php8.3-fpm.service"},
	}); err == nil {
		t.Error("unidade inativa foi aceita como ativa")
	}

	enabled := SystemVerifier{Runner: fakeOutput{outputs: map[string]string{
		"systemctl is-enabled php8.3-fpm.service": "enabled\n",
	}}}
	if err := enabled.Verify(context.Background(), core.Step{
		Verify:     core.VerifyUnitEnabled,
		VerifyArgs: []string{"php8.3-fpm.service"},
	}); err != nil {
		t.Fatalf("unidade habilitada: %v", err)
	}
	disabled := SystemVerifier{Runner: fakeOutput{
		outputs: map[string]string{"systemctl is-enabled php8.3-fpm.service": "disabled\n"},
		errs:    map[string]error{"systemctl is-enabled php8.3-fpm.service": errors.New("exit status 1")},
	}}
	if err := disabled.Verify(context.Background(), core.Step{
		Verify:     core.VerifyUnitEnabled,
		VerifyArgs: []string{"php8.3-fpm.service"},
	}); err == nil {
		t.Error("unidade desabilitada foi aceita")
	}
}

func TestSystemVerifierAlternativeCurrent(t *testing.T) {
	out := "Link: /usr/bin/php\nValue: /usr/bin/php8.3\nAlternative: /usr/bin/php8.2\nPriority: 20\n"
	v := SystemVerifier{Runner: fakeOutput{outputs: map[string]string{
		"update-alternatives --query php": out,
	}}}
	if err := v.Verify(context.Background(), core.Step{
		Verify:     core.VerifyAlternativeCurrent,
		VerifyArgs: []string{"php", "/usr/bin/php8.3"},
	}); err != nil {
		t.Fatalf("alternative correta: %v", err)
	}
	if err := v.Verify(context.Background(), core.Step{
		Verify:     core.VerifyAlternativeCurrent,
		VerifyArgs: []string{"php", "/usr/bin/php8.2"},
	}); err == nil {
		t.Error("alternative divergente foi aceita")
	}
}

func TestSystemVerifierSocket(t *testing.T) {
	path := t.TempDir() + "/php-fpm.sock"
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	v := SystemVerifier{}
	if err := v.Verify(context.Background(), core.Step{
		Verify:     core.VerifySocketExists,
		VerifyArgs: []string{path},
	}); err != nil {
		t.Fatalf("socket existente: %v", err)
	}
	if err := v.Verify(context.Background(), core.Step{
		Verify:     core.VerifySocketExists,
		VerifyArgs: []string{path + ".missing"},
	}); err == nil {
		t.Error("socket ausente foi aceito")
	}
}

func TestSystemVerifierRejectsBadArguments(t *testing.T) {
	v := SystemVerifier{Runner: fakeOutput{}}
	steps := []core.Step{
		{Verify: core.VerifyPackagesInstalled},
		{Verify: core.VerifyBinaryVersion, VerifyArgs: []string{"/usr/bin/php"}},
		{Verify: core.VerifyUnitActive, VerifyArgs: []string{"a", "b"}},
		{Verify: core.VerifySocketExists},
		{Verify: core.VerifyAlternativeCurrent, VerifyArgs: []string{"php"}},
		{Verify: core.VerifyKind("unknown")},
	}
	for _, step := range steps {
		if err := v.Verify(context.Background(), step); err == nil {
			t.Errorf("verificação inválida aceita: %+v", step)
		}
	}
}
