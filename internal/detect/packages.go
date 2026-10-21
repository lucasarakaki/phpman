package detect

import (
	"context"
	"strings"
	"time"

	"github.com/lucasarakaki/phpman/internal/core"
)

// DetectPackages lists installed OS packages whose name starts with "php".
func DetectPackages(ctx context.Context) ([]core.Package, []string) {
	switch {
	case hasCommand("dpkg-query"):
		return detectDpkg(ctx)
	case hasCommand("rpm"):
		return detectRPM(ctx)
	default:
		return nil, []string{"gerenciador de pacotes não reconhecido; pacotes PHP não listados"}
	}
}

func detectDpkg(ctx context.Context) ([]core.Package, []string) {
	format := `${Status}\t${Package}\t${Version}\n`
	out, err := run(ctx, 8*time.Second, "dpkg-query", "-W", "-f="+format, "php*")
	if err != nil && out == "" {
		return nil, []string{"dpkg-query falhou ao listar pacotes PHP"}
	}

	var pkgs []core.Package
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		if !strings.Contains(fields[0], "installed") {
			continue
		}
		name := strings.TrimSpace(fields[1])
		if !strings.HasPrefix(name, "php") {
			continue
		}
		pkgs = append(pkgs, core.Package{Name: name, Version: strings.TrimSpace(fields[2]), Manager: "dpkg"})
	}
	return pkgs, nil
}

func detectRPM(ctx context.Context) ([]core.Package, []string) {
	out, err := run(ctx, 8*time.Second, "rpm", "-qa", "--qf", `%{NAME}\t%{VERSION}-%{RELEASE}\n`, "php*")
	if err != nil && out == "" {
		return nil, []string{"rpm falhou ao listar pacotes PHP"}
	}

	var pkgs []core.Package
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSpace(fields[0])
		if !strings.HasPrefix(name, "php") {
			continue
		}
		pkgs = append(pkgs, core.Package{Name: name, Version: strings.TrimSpace(fields[1]), Manager: "rpm"})
	}
	return pkgs, nil
}

// PackagesForVersion selects the packages that belong to one X.Y version.
func PackagesForVersion(all []core.Package, raw string) []core.Package {
	var out []core.Package
	compact := strings.ReplaceAll(raw, ".", "")
	for _, p := range all {
		name := p.Name
		switch {
		case strings.HasPrefix(name, "php"+raw+"-"), name == "php"+raw,
			strings.HasPrefix(name, "php"+compact+"-"), name == "php"+compact:
			out = append(out, p)
		}
	}
	return out
}
