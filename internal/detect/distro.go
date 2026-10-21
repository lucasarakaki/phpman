package detect

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lucasarakaki/phpman/internal/core"
)

// osReleasePaths is ordered by preference.
var osReleasePaths = []string{"/etc/os-release", "/usr/lib/os-release"}

// DetectDistro reads os-release from the well known locations.
func DetectDistro() core.Distro {
	for _, path := range osReleasePaths {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		d, err := ParseOSRelease(f)
		_ = f.Close()
		if err != nil {
			continue
		}
		return d
	}
	return core.Distro{
		ID:             "unknown",
		Name:           "unknown",
		PrettyName:     "distribuição desconhecida",
		Family:         core.FamilyUnknown,
		PackageManager: "",
	}
}

// ParseOSRelease parses the shell-like key=value format of os-release.
func ParseOSRelease(r io.Reader) (core.Distro, error) {
	values := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, raw, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = unquote(strings.TrimSpace(raw))
	}
	if err := sc.Err(); err != nil {
		return core.Distro{}, err
	}
	if values["ID"] == "" && values["NAME"] == "" {
		return core.Distro{}, fmt.Errorf("os-release sem ID ou NAME")
	}

	d := core.Distro{
		ID:         values["ID"],
		Name:       values["NAME"],
		PrettyName: values["PRETTY_NAME"],
		VersionID:  values["VERSION_ID"],
		Codename:   values["VERSION_CODENAME"],
	}
	if d.Codename == "" {
		d.Codename = values["UBUNTU_CODENAME"]
	}
	if like := strings.TrimSpace(values["ID_LIKE"]); like != "" {
		d.Like = strings.Fields(like)
	}
	if d.PrettyName == "" {
		d.PrettyName = strings.TrimSpace(d.Name + " " + d.VersionID)
	}
	if d.ID == "" {
		d.ID = strings.ToLower(strings.Fields(d.Name)[0])
	}
	d.Family, d.PackageManager = classifyDistro(d.ID, d.Like)
	return d, nil
}

// classifyDistro maps an ID plus ID_LIKE list to a packaging family.
func classifyDistro(id string, like []string) (core.Family, string) {
	ids := append([]string{strings.ToLower(id)}, like...)
	for _, candidate := range ids {
		switch candidate {
		case "debian", "ubuntu", "linuxmint", "pop", "elementary", "zorin",
			"kali", "raspbian", "devuan", "neon", "tuxedo", "deepin", "mx":
			return core.FamilyDebian, "apt"
		case "fedora":
			return core.FamilyFedora, "dnf"
		case "rhel", "centos", "rocky", "almalinux", "ol", "amzn", "oracle":
			return core.FamilyRHEL, "dnf"
		case "arch", "manjaro", "endeavouros", "cachyos", "garuda":
			return core.FamilyArch, "pacman"
		case "opensuse", "opensuse-leap", "opensuse-tumbleweed", "suse", "sles", "sled":
			return core.FamilySUSE, "zypper"
		case "alpine":
			return core.FamilyAlpine, "apk"
		}
	}
	return core.FamilyUnknown, ""
}

// FamilyLabel returns a human readable name for a family.
func FamilyLabel(f core.Family) string {
	switch f {
	case core.FamilyDebian:
		return "Debian/Ubuntu (apt)"
	case core.FamilyFedora:
		return "Fedora (dnf)"
	case core.FamilyRHEL:
		return "RHEL e derivados (dnf)"
	case core.FamilyArch:
		return "Arch e derivados (pacman)"
	case core.FamilySUSE:
		return "openSUSE/SLES (zypper)"
	case core.FamilyAlpine:
		return "Alpine (apk)"
	default:
		return "desconhecida"
	}
}
