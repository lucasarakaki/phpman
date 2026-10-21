package detect

import (
	"strings"
	"testing"

	"github.com/lucasarakaki/phpman/internal/core"
)

func TestParseOSRelease(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		family   core.Family
		manager  string
		codename string
	}{
		{
			name: "ubuntu",
			input: `NAME="Ubuntu"
VERSION="24.04.1 LTS (Noble Numbat)"
ID=ubuntu
ID_LIKE=debian
PRETTY_NAME="Ubuntu 24.04.1 LTS"
VERSION_ID="24.04"
VERSION_CODENAME=noble
`,
			family:   core.FamilyDebian,
			manager:  "apt",
			codename: "noble",
		},
		{
			name: "debian",
			input: `PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"
NAME="Debian GNU/Linux"
VERSION_ID="12"
VERSION_CODENAME=bookworm
ID=debian
`,
			family:   core.FamilyDebian,
			manager:  "apt",
			codename: "bookworm",
		},
		{
			name: "fedora",
			input: `NAME="Fedora Linux"
VERSION="44 (Workstation Edition)"
ID=fedora
VERSION_ID=44
PRETTY_NAME="Fedora Linux 44 (Workstation Edition)"
`,
			family:  core.FamilyFedora,
			manager: "dnf",
		},
		{
			name: "arch",
			input: `NAME="Arch Linux"
PRETTY_NAME="Arch Linux"
ID=arch
ID_LIKE=archlinux
`,
			family:  core.FamilyArch,
			manager: "pacman",
		},
		{
			name: "ubuntu derivative through id_like",
			input: `NAME="Linux Mint"
ID=linuxmint
ID_LIKE="ubuntu debian"
VERSION_ID="22"
`,
			family:  core.FamilyDebian,
			manager: "apt",
		},
		{
			name: "alpine",
			input: `NAME="Alpine Linux"
ID=alpine
VERSION_ID=3.20.3
PRETTY_NAME="Alpine Linux v3.20"
`,
			family:  core.FamilyAlpine,
			manager: "apk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := ParseOSRelease(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("ParseOSRelease: %v", err)
			}
			if d.Family != tt.family {
				t.Errorf("family = %q, want %q", d.Family, tt.family)
			}
			if d.PackageManager != tt.manager {
				t.Errorf("manager = %q, want %q", d.PackageManager, tt.manager)
			}
			if tt.codename != "" && d.Codename != tt.codename {
				t.Errorf("codename = %q, want %q", d.Codename, tt.codename)
			}
			if d.PrettyName == "" {
				t.Error("PrettyName vazio")
			}
		})
	}
}

func TestParseOSReleaseEmpty(t *testing.T) {
	if _, err := ParseOSRelease(strings.NewReader("")); err == nil {
		t.Fatal("esperava erro para os-release vazio")
	}
}
