package executor

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
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

type fakeOutputFunc func(context.Context, []string) (string, error)

func (f fakeOutputFunc) Run(ctx context.Context, argv []string) (string, error) {
	return f(ctx, argv)
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

func TestSystemVerifierFileContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sury.list")
	content := "deb [signed-by=/usr/share/keyrings/deb.sury.org-php.gpg] https://packages.sury.org/php/ bookworm main\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	v := SystemVerifier{}
	step := core.Step{
		File:   &core.FileWrite{Path: path, Content: content, Mode: 0o644},
		Verify: core.VerifyFileContent,
	}
	if err := v.Verify(context.Background(), step); err != nil {
		t.Fatalf("conteúdo correto: %v", err)
	}
	step.File.Content = content + "extra\n"
	if err := v.Verify(context.Background(), step); err == nil {
		t.Error("conteúdo divergente foi aceito")
	}
	if err := v.Verify(context.Background(), core.Step{Verify: core.VerifyFileContent}); err == nil {
		t.Error("verificação sem arquivo foi aceita")
	}
}

func TestSystemVerifierPGPKey(t *testing.T) {
	var gotArgv []string
	runner := fakeOutputFunc(func(_ context.Context, argv []string) (string, error) {
		gotArgv = argv
		return "pub:u:3072:1:ABC:0:0:::::::\nfpr:::::::::15058500A0235D97F5D10063B188E2B695BD4743:\n", nil
	})
	v := SystemVerifier{Runner: runner}
	step := core.Step{
		Verify:     core.VerifyPGPKey,
		VerifyArgs: []string{suryKeyring, "15058500A0235D97F5D10063B188E2B695BD4743"},
	}
	if err := v.Verify(context.Background(), step); err != nil {
		t.Fatalf("fingerprint correto: %v", err)
	}
	if len(gotArgv) < 2 || gotArgv[0] != "/usr/bin/gpg" || gotArgv[1] != "--homedir" {
		t.Errorf("argv do gpg = %#v", gotArgv)
	}
	step.VerifyArgs[1] = "0000000000000000000000000000000000000000"
	if err := v.Verify(context.Background(), step); err == nil {
		t.Error("fingerprint divergente foi aceito")
	}
	if err := v.Verify(context.Background(), core.Step{Verify: core.VerifyPGPKey, VerifyArgs: []string{suryKeyring}}); err == nil {
		t.Error("verificação sem fingerprint foi aceita")
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
