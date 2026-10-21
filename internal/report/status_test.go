package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lucasarakaki/phpman/internal/core"
)

func fixture() *core.Snapshot {
	return &core.Snapshot{
		Schema:        core.Schema,
		Distro:        core.Distro{PrettyName: "Debian GNU/Linux 12 (bookworm)", ID: "debian", Family: core.FamilyDebian, PackageManager: "apt"},
		ActiveVersion: "8.2",
		PathVersion:   "8.2",
		PathBinary:    "/usr/bin/php8.2",
		Versions: []core.Version{{
			Raw: "8.2", Full: "8.2.29", Active: true, InPath: true, Path: "/usr/bin/php8.2",
			SAPIs: []core.SAPI{
				{Kind: core.SAPICLI, Binary: "/usr/bin/php8.2", IniFile: "/etc/php/8.2/cli/php.ini"},
				{Kind: core.SAPIFPM, Binary: "/usr/sbin/php-fpm8.2", Unit: "php8.2-fpm.service",
					Active: true, Enabled: true, State: "active",
					Socket: "/run/php/php8.2-fpm.sock", SocketExists: true},
			},
			Extensions: []core.Extension{{Name: "curl", Loaded: true}, {Name: "mbstring", Loaded: true}},
			Packages:   []core.Package{{Name: "php8.2-cli", Version: "8.2.29", Manager: "dpkg"}},
		}},
		Available: []core.AvailableVersion{{Raw: "8.3", Package: "php8.3-cli", Candidate: "8.3.27", Origin: "terceiros", Source: "packages.sury.org/php"}},
		Services:  []core.Service{{Unit: "php8.2-fpm.service", Version: "8.2", Active: true, Enabled: true, State: "active"}},
		Sockets:   []core.Socket{{Path: "/run/php/php8.2-fpm.sock", Version: "8.2", Exists: true}},
		Vhosts:    []core.Vhost{{File: "/etc/nginx/sites-enabled/app.conf", Line: 12, Server: "app.test", Kind: "fpm-socket", Target: "/run/php/php8.2-fpm.sock"}},
		Warnings: []core.Warning{{
			Severity: core.SeverityWarn, Code: "fpm-inactive",
			Message: "O FPM do PHP 8.2 está parado.", Hint: "sudo systemctl start php8.2-fpm.service",
		}},
		Capabilities: []core.Capability{
			{Key: "detect", Label: "Detectar", Supported: true, Detail: "somente leitura", Phase: "M0"},
			{Key: "install", Label: "Instalar", Supported: false, Detail: "planejado", Phase: "M1"},
		},
	}
}

func TestWriteText(t *testing.T) {
	var buf bytes.Buffer
	WriteText(&buf, fixture(), "test", false)
	out := buf.String()

	for _, want := range []string{
		"PHP EM USO", "VERSÕES INSTALADAS", "8.2.29", "SITES QUE USAM PHP",
		"DIAGNÓSTICO", "fpm-inactive", "CAPACIDADES", "8.3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("saída não contém %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("cores apareceram com color=false")
	}
}

func TestWriteTextWithColor(t *testing.T) {
	var buf bytes.Buffer
	WriteText(&buf, fixture(), "test", true)
	if !strings.Contains(buf.String(), "\x1b[") {
		t.Error("esperava códigos ANSI com color=true")
	}
}

func TestWriteJSONRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, fixture()); err != nil {
		t.Fatal(err)
	}
	var got core.Snapshot
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("JSON inválido: %v", err)
	}
	if got.Schema != core.Schema {
		t.Errorf("schema = %d", got.Schema)
	}
	if len(got.Versions) != 1 || got.Versions[0].Raw != "8.2" {
		t.Errorf("versões = %+v", got.Versions)
	}
}

func TestWriteDoctorClean(t *testing.T) {
	snap := fixture()
	snap.Warnings = nil
	var buf bytes.Buffer
	WriteDoctor(&buf, snap, false)
	if !strings.Contains(buf.String(), "Nenhum problema encontrado") {
		t.Errorf("saída inesperada:\n%s", buf.String())
	}
}

func TestWriteDoctorWithWarnings(t *testing.T) {
	var buf bytes.Buffer
	WriteDoctor(&buf, fixture(), false)
	out := buf.String()
	if !strings.Contains(out, "fpm-inactive") || !strings.Contains(out, "1 aviso") {
		t.Errorf("saída inesperada:\n%s", out)
	}
}

func TestWrapString(t *testing.T) {
	lines := wrapString("aaaa bbbb cccc dddd", 10)
	if len(lines) != 2 {
		t.Fatalf("lines = %v", lines)
	}
	if lines[0] != "aaaa bbbb" {
		t.Errorf("lines[0] = %q", lines[0])
	}
}
