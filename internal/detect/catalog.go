package detect

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lucasarakaki/phpman/internal/core"
)

var aptCLIPackageRe = regexp.MustCompile(`^php(\d+)\.(\d+)-cli$`)
var urlRe = regexp.MustCompile(`https?://\S+`)

// DetectAvailable lists the PHP versions the package manager can install.
// Only apt is implemented in M0; other families get an empty list and an
// explanatory capability entry.
func DetectAvailable(ctx context.Context, distro core.Distro, installed []core.Version) ([]core.AvailableVersion, []string) {
	if distro.Family != core.FamilyDebian {
		return nil, nil
	}
	if !hasCommand("apt-cache") {
		return nil, []string{"apt-cache não encontrado; catálogo de versões indisponível"}
	}

	out, err := run(ctx, 10*time.Second, "apt-cache", "search", "--names-only", `^php[0-9]+\.[0-9]+-cli$`)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, []string{"não foi possível consultar o catálogo apt; verifique se as listas de pacotes estão atualizadas"}
	}

	names := ParseAPTSearch(out)
	installedRaw := map[string]bool{}
	for _, v := range installed {
		installedRaw[v.Raw] = true
	}

	var available []core.AvailableVersion
	seen := map[string]bool{}
	for _, name := range names {
		m := aptCLIPackageRe.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		raw := m[1] + "." + m[2]
		if seen[raw] {
			continue
		}
		seen[raw] = true

		av := core.AvailableVersion{Raw: raw, Package: name, Installed: installedRaw[raw], Origin: "distro"}
		if policy, err := run(ctx, 4*time.Second, "apt-cache", "policy", name); err == nil {
			av.Candidate, av.Source, av.Origin = ParseAPTPolicy(policy)
			if av.Origin == "" {
				av.Origin = "distro"
			}
		}
		available = append(available, av)
	}

	sort.Slice(available, func(i, j int) bool {
		return versionLess(available[i].Raw, available[j].Raw)
	})
	return available, nil
}

// ParseAPTSearch extracts package names from `apt-cache search --names-only`.
func ParseAPTSearch(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		name, _, ok := strings.Cut(strings.TrimSpace(line), " - ")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, " \t") {
			continue
		}
		names = append(names, name)
	}
	return names
}

// ParseAPTPolicy extracts the candidate version, the repository URL and an
// origin label from `apt-cache policy`.
func ParseAPTPolicy(out string) (candidate, source, origin string) {
	origin = "distro"
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Candidate:"):
			candidate = strings.TrimSpace(strings.TrimPrefix(trimmed, "Candidate:"))
		case strings.Contains(trimmed, "http://"), strings.Contains(trimmed, "https://"):
			url := urlRe.FindString(trimmed)
			if url == "" {
				continue
			}
			source = compactURL(url)
			if strings.Contains(url, "sury.org") || strings.Contains(url, "ondrej") {
				origin = "terceiros"
			}
		}
	}
	if candidate == "(none)" {
		candidate = ""
	}
	return candidate, source, origin
}

func compactURL(url string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
	parts := strings.Split(s, "/")
	if len(parts) == 1 {
		return parts[0]
	}
	return parts[0] + "/" + parts[1]
}

func versionLess(a, b string) bool {
	am, an := splitRaw(a)
	bm, bn := splitRaw(b)
	if am != bm {
		return am < bm
	}
	return an < bn
}
