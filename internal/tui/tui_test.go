package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucasarakaki/phpman/internal/core"
)

func fixtureSnapshot() *core.Snapshot {
	return &core.Snapshot{
		Schema:        core.Schema,
		Distro:        core.Distro{ID: "debian", PrettyName: "Debian GNU/Linux 12 (bookworm)", Family: core.FamilyDebian, PackageManager: "apt"},
		ActiveVersion: "8.2",
		PathVersion:   "8.2",
		PathBinary:    "/usr/bin/php8.2",
		Versions: []core.Version{{
			Raw: "8.2", Full: "8.2.29", Active: true, InPath: true, Path: "/usr/bin/php8.2",
			SAPIs: []core.SAPI{
				{Kind: core.SAPICLI, Binary: "/usr/bin/php8.2", IniFile: "/etc/php/8.2/cli/php.ini"},
				{Kind: core.SAPIFPM, Unit: "php8.2-fpm.service", Active: true, State: "active",
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
		Capabilities: []core.Capability{{Key: "detect", Label: "Detectar", Supported: true, Detail: "somente leitura", Phase: "M0"}},
	}
}

func fixtureModel(width, height int) Model {
	m := New(context.Background(), "test")
	m.snap = fixtureSnapshot()
	m.loading = false
	m.width, m.height = width, height
	return m
}

func TestViewAllTabs(t *testing.T) {
	m := fixtureModel(120, 40)
	for _, tab := range []tab{tabOverview, tabVersions, tabDoctor} {
		m.tab = tab
		out := m.View()
		if strings.TrimSpace(out) == "" {
			t.Fatalf("aba %d vazia", tab)
		}
		if !strings.Contains(out, "phpman") {
			t.Errorf("aba %d sem cabeçalho: %q", tab, out)
		}
	}
}

func TestVersionsTabListsInstalledAndAvailable(t *testing.T) {
	m := fixtureModel(120, 40)
	m.tab = tabVersions
	out := m.View()
	if !strings.Contains(out, "8.2.29") {
		t.Errorf("versão instalada ausente:\n%s", out)
	}
	if !strings.Contains(out, "8.3") {
		t.Errorf("versão disponível ausente:\n%s", out)
	}
}

func TestDoctorTabShowsWarning(t *testing.T) {
	m := fixtureModel(120, 40)
	m.tab = tabDoctor
	out := m.View()
	if !strings.Contains(out, "fpm-inactive") || !strings.Contains(out, "systemctl") {
		t.Errorf("diagnóstico incompleto:\n%s", out)
	}
}

func TestNarrowTerminalDoesNotPanic(t *testing.T) {
	m := fixtureModel(60, 24)
	for _, tab := range []tab{tabOverview, tabVersions, tabDoctor} {
		m.tab = tab
		if out := m.View(); strings.TrimSpace(out) == "" {
			t.Fatalf("aba %d vazia em terminal estreito", tab)
		}
	}
}

func TestEmptySnapshotDoesNotPanic(t *testing.T) {
	m := fixtureModel(100, 30)
	m.snap = &core.Snapshot{Distro: core.Distro{PrettyName: "Fedora Linux 44", Family: core.FamilyFedora}}
	for _, tab := range []tab{tabOverview, tabVersions, tabDoctor} {
		m.tab = tab
		if out := m.View(); strings.TrimSpace(out) == "" {
			t.Fatalf("aba %d vazia sem dados", tab)
		}
	}
}

func TestHelpOverlay(t *testing.T) {
	m := fixtureModel(100, 30)
	m.help = true
	if out := m.View(); !strings.Contains(out, "Atalhos") {
		t.Errorf("ajuda ausente:\n%s", out)
	}
}

func TestKeyNavigation(t *testing.T) {
	m := fixtureModel(100, 30)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.tab != tabVersions {
		t.Errorf("tab = %d, want %d", m.tab, tabVersions)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m = updated.(Model)
	if m.tab != tabDoctor {
		t.Errorf("tab = %d, want %d", m.tab, tabDoctor)
	}

	m.tab = tabVersions
	m.cursor = 0
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1", m.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	if !m.loading {
		t.Error("r deveria iniciar recarga")
	}
}

func TestLoadedMsg(t *testing.T) {
	m := fixtureModel(100, 30)
	m.loading = true
	updated, _ := m.Update(loadedMsg{id: m.reload, snap: fixtureSnapshot()})
	m = updated.(Model)
	if m.loading {
		t.Error("loading deveria ser false")
	}
	if m.snap == nil {
		t.Error("snapshot não aplicado")
	}
}

func TestWrapWords(t *testing.T) {
	got := wrapWords("alpha beta gamma", 10)
	if got != "alpha beta\ngamma" {
		t.Errorf("wrapWords = %q", got)
	}
	long := wrapWords("abcdefghijkl", 4)
	if long != "abcd\nefgh\nijkl" {
		t.Errorf("hard break = %q", long)
	}
}
