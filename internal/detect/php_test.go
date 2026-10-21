package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseVersionOutput(t *testing.T) {
	tests := []struct {
		input string
		major int
		minor int
		patch int
		sapi  string
	}{
		{"PHP 8.5.11 (cli) (built: Sep 22 2026 13:32:06) (NTS gcc x86_64)", 8, 5, 11, "cli"},
		{"PHP 8.2.29 (fpm-fcgi) (built: Jan 1 2026)", 8, 2, 29, "fpm-fcgi"},
		{"PHP Warning: something\nPHP 7.4.33 (cgi-fcgi) (built: ...)", 7, 4, 33, "cgi-fcgi"},
	}
	for _, tt := range tests {
		major, minor, patch, sapi, ok := ParseVersionOutput(tt.input)
		if !ok {
			t.Fatalf("não reconheceu %q", tt.input)
		}
		if major != tt.major || minor != tt.minor || patch != tt.patch || sapi != tt.sapi {
			t.Errorf("%q => %d.%d.%d %q, want %d.%d.%d %q",
				tt.input, major, minor, patch, sapi, tt.major, tt.minor, tt.patch, tt.sapi)
		}
	}
}

func TestParseVersionOutputRejectsNoise(t *testing.T) {
	if _, _, _, _, ok := ParseVersionOutput("bash: php: command not found"); ok {
		t.Fatal("não deveria reconhecer saída sem versão")
	}
}

func TestParseModules(t *testing.T) {
	out := `[PHP Modules]
Core
curl
mbstring
Zend OPcache

[Zend Modules]
Zend OPcache
`
	exts := ParseModules(out)
	if len(exts) != 4 {
		t.Fatalf("len = %d, want 4 (%v)", len(exts), exts)
	}
	if exts[0].Name != "Core" || !exts[0].Loaded {
		t.Errorf("primeira extensão inesperada: %+v", exts[0])
	}
	for _, e := range exts {
		if e.Name == "[PHP Modules]" || e.Name == "" {
			t.Errorf("cabeçalho vazou: %+v", e)
		}
	}
}

func TestParseIni(t *testing.T) {
	out := `Configuration File (php.ini) Path: /etc/php/8.2/cli
Loaded Configuration File:         /etc/php/8.2/cli/php.ini
Scan for additional .ini files in: /etc/php/8.2/cli/conf.d
Additional .ini files parsed:      /etc/php/8.2/cli/conf.d/10-mysqlnd.ini,
/etc/php/8.2/cli/conf.d/20-curl.ini
`
	file, scanDir, extra := ParseIni(out)
	if file != "/etc/php/8.2/cli/php.ini" {
		t.Errorf("file = %q", file)
	}
	if scanDir != "/etc/php/8.2/cli/conf.d" {
		t.Errorf("scanDir = %q", scanDir)
	}
	if len(extra) != 2 {
		t.Errorf("extra = %v", extra)
	}
}

func TestParseIniNone(t *testing.T) {
	file, scanDir, _ := ParseIni("Loaded Configuration File: (none)\n")
	if file != "" || scanDir != "" {
		t.Errorf("esperava vazio, obtive %q %q", file, scanDir)
	}
}

func TestDiscoverBinariesClassifiesAndDeduplicates(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk("php8.2")
	mk("php-fpm8.2")
	mk("php-cgi8.2")
	mk("phpize")
	mk("php-config")

	if err := os.Symlink(filepath.Join(dir, "php8.2"), filepath.Join(dir, "php")); err != nil {
		t.Fatal(err)
	}

	bins := DiscoverBinaries([]string{dir})
	if len(bins) != 3 {
		t.Fatalf("len = %d, want 3 (%+v)", len(bins), bins)
	}
	kinds := map[string]bool{}
	for _, b := range bins {
		kinds[string(b.Kind)] = true
	}
	for _, want := range []string{"cli", "fpm", "cgi"} {
		if !kinds[want] {
			t.Errorf("SAPI %q não detectada: %+v", want, bins)
		}
	}
}

func TestVersionFromBinaryPath(t *testing.T) {
	cases := map[string]string{
		"/usr/bin/php8.2":       "8.2",
		"/usr/bin/php":          "",
		"/usr/bin/php-fpm8.3":   "8.3",
		"/usr/local/bin/php7.4": "7.4",
	}
	for path, want := range cases {
		if got := VersionFromBinaryPath(path); got != want {
			t.Errorf("VersionFromBinaryPath(%q) = %q, want %q", path, got, want)
		}
	}
}
