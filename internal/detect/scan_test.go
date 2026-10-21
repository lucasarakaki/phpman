package detect

import (
	"strings"
	"testing"

	"github.com/lucasarakaki/phpman/internal/core"
)

func TestParseAlternativesQuery(t *testing.T) {
	out := `Name: php
Link: /usr/bin/php
Slaves:
 phpize /usr/bin/phpize
Status: auto
Best: /usr/bin/php8.3
Value: /usr/bin/php8.2

Alternative: /usr/bin/php8.2
Priority: 82
`
	alt, ok := ParseAlternativesQuery(out)
	if !ok {
		t.Fatal("esperava alternativa")
	}
	if alt.Name != "php" || alt.Link != "/usr/bin/php" {
		t.Errorf("alt = %+v", alt)
	}
	if alt.Current != "/usr/bin/php8.2" || alt.Best != "/usr/bin/php8.3" || alt.Status != "auto" {
		t.Errorf("alt = %+v", alt)
	}
}

func TestParseAPTSearch(t *testing.T) {
	out := `php8.3-cli - command-line interpreter for the PHP scripting language
php8.2-cli - command-line interpreter for the PHP scripting language
php8.3-common - documentation, examples and common module for PHP
`
	names := ParseAPTSearch(out)
	if len(names) != 3 {
		t.Fatalf("names = %v", names)
	}
	if names[0] != "php8.3-cli" {
		t.Errorf("names[0] = %q", names[0])
	}
}

func TestParseAPTPolicy(t *testing.T) {
	out := `php8.3-cli:
  Installed: (none)
  Candidate: 8.3.27-1+ubuntu24.04.1+deb.sury.org+1
  Version table:
     8.3.27-1+ubuntu24.04.1+deb.sury.org+1 500
        500 https://packages.sury.org/php noble/main amd64 Packages
        100 /var/lib/dpkg/status
`
	candidate, source, origin := ParseAPTPolicy(out)
	if candidate != "8.3.27-1+ubuntu24.04.1+deb.sury.org+1" {
		t.Errorf("candidate = %q", candidate)
	}
	if source != "packages.sury.org/php" {
		t.Errorf("source = %q", source)
	}
	if origin != "terceiros" {
		t.Errorf("origin = %q", origin)
	}
}

func TestParseAPTPolicyDistro(t *testing.T) {
	out := `php8.3-cli:
  Installed: 8.3.6-0ubuntu0.24.04.5
  Candidate: 8.3.6-0ubuntu0.24.04.5
  Version table:
 *** 8.3.6-0ubuntu0.24.04.5 500
        500 http://archive.ubuntu.com/ubuntu noble-updates/main amd64 Packages
`
	candidate, source, origin := ParseAPTPolicy(out)
	if candidate != "8.3.6-0ubuntu0.24.04.5" {
		t.Errorf("candidate = %q", candidate)
	}
	if source != "archive.ubuntu.com/ubuntu" {
		t.Errorf("source = %q", source)
	}
	if origin != "distro" {
		t.Errorf("origin = %q", origin)
	}
}

func TestPackagesForVersion(t *testing.T) {
	pkgs := []core.Package{
		{Name: "php8.2-cli", Version: "8.2.29", Manager: "dpkg"},
		{Name: "php8.2-fpm", Version: "8.2.29", Manager: "dpkg"},
		{Name: "php-cli", Version: "8.3.6", Manager: "dpkg"},
		{Name: "php8.3-cli", Version: "8.3.6", Manager: "dpkg"},
		{Name: "php82-php-fpm", Version: "8.2.29", Manager: "rpm"},
	}
	got := PackagesForVersion(pkgs, "8.2")
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	for _, p := range got {
		if strings.Contains(p.Name, "8.3") {
			t.Errorf("pacote de outra versão: %+v", p)
		}
	}
}

func TestBuildWarningsNoPHP(t *testing.T) {
	s := &core.Snapshot{Distro: core.Distro{Family: core.FamilyDebian}}
	warnings := BuildWarnings(s)
	if !hasCode(warnings, "no-php") {
		t.Fatalf("esperava no-php, obtive %+v", warnings)
	}
}

func TestBuildWarningsPathMismatch(t *testing.T) {
	s := &core.Snapshot{
		Distro:        core.Distro{Family: core.FamilyDebian},
		ActiveVersion: "8.2",
		PathVersion:   "8.3",
		PathBinary:    "/usr/bin/php",
		Versions: []core.Version{
			{Raw: "8.2", Active: true},
			{Raw: "8.3"},
		},
	}
	warnings := BuildWarnings(s)
	if !hasCode(warnings, "path-mismatch") {
		t.Fatalf("esperava path-mismatch, obtive %+v", warnings)
	}
}

func TestBuildWarningsMissingVhostSocket(t *testing.T) {
	s := &core.Snapshot{
		Distro:        core.Distro{Family: core.FamilyDebian},
		ActiveVersion: "8.2",
		PathVersion:   "8.2",
		Versions: []core.Version{{
			Raw:    "8.2",
			Active: true,
			SAPIs: []core.SAPI{{
				Kind: core.SAPIFPM, Unit: "php8.2-fpm.service", Active: true, State: "active",
				Socket: "/run/php/php8.2-fpm.sock", SocketExists: false,
			}},
		}},
		Sockets: []core.Socket{{Path: "/run/php/php8.2-fpm.sock", Version: "8.2", Exists: false}},
		Vhosts:  []core.Vhost{{File: "/etc/nginx/sites-enabled/app.conf", Line: 12, Kind: "fpm-socket", Target: "/run/php/php8.2-fpm.sock"}},
	}
	warnings := BuildWarnings(s)
	if !hasCode(warnings, "socket-missing") {
		t.Errorf("esperava socket-missing, obtive %+v", warnings)
	}
	if !hasCode(warnings, "vhost-socket-missing") {
		t.Errorf("esperava vhost-socket-missing, obtive %+v", warnings)
	}
}

func TestBuildWarningsInactiveFPMDoesNotDoubleReportSocket(t *testing.T) {
	s := &core.Snapshot{
		Distro:        core.Distro{Family: core.FamilyDebian},
		ActiveVersion: "8.2",
		PathVersion:   "8.2",
		Versions: []core.Version{{
			Raw: "8.2",
			SAPIs: []core.SAPI{{
				Kind: core.SAPIFPM, Unit: "php8.2-fpm.service", Active: false, State: "inactive",
				Socket: "/run/php/php8.2-fpm.sock", SocketExists: false,
			}},
		}},
		Sockets: []core.Socket{{Path: "/run/php/php8.2-fpm.sock", Version: "8.2", Exists: false}},
	}
	warnings := BuildWarnings(s)
	if hasCode(warnings, "socket-missing") {
		t.Errorf("socket ausente não deveria ser reportado com FPM parado: %+v", warnings)
	}
}

func TestCapabilities(t *testing.T) {
	debian := Capabilities(core.Distro{Family: core.FamilyDebian}, true)
	fedora := Capabilities(core.Distro{Family: core.FamilyFedora}, true)
	if !capSupported(debian, "catalog") {
		t.Error("catálogo deveria ser suportado no Debian com apt-cache")
	}
	if capSupported(fedora, "catalog") {
		t.Error("catálogo não deveria ser suportado no Fedora")
	}
	noApt := Capabilities(core.Distro{Family: core.FamilyDebian}, false)
	if capSupported(noApt, "catalog") {
		t.Error("catálogo não deveria aparecer sem apt-cache")
	}
}

func hasCode(warnings []core.Warning, code string) bool {
	for _, w := range warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}

func capSupported(caps []core.Capability, key string) bool {
	for _, c := range caps {
		if c.Key == key {
			return c.Supported
		}
	}
	return false
}
