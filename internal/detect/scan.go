package detect

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lucasarakaki/phpman/internal/core"
)

// Scan builds a complete read-only snapshot of the machine. It never mutates
// state and never requires root.
func Scan(ctx context.Context) (*core.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s := &core.Snapshot{Schema: core.Schema, GeneratedAt: time.Now(), Distro: DetectDistro()}

	// Independent probes run concurrently: the slowest ones (rpm/dpkg and the
	// systemd query) would otherwise dominate the wall clock.
	var (
		wg sync.WaitGroup

		pkgs     []core.Package
		versions []core.Version
		services []core.Service
		sockets  []core.Socket
		alts     []core.Alternative
		vhosts   []core.Vhost
		mods     []ApacheModule
		pathBin  string
		pathVer  string

		pkgNotes, phpNotes, svcNotes, sockNotes, altNotes []string
	)

	wg.Add(8)
	go func() { defer wg.Done(); pkgs, pkgNotes = DetectPackages(ctx) }()
	go func() { defer wg.Done(); versions, phpNotes = DetectPHP(ctx) }()
	go func() { defer wg.Done(); services, svcNotes = DetectServices(ctx) }()
	go func() { defer wg.Done(); sockets, sockNotes = DetectSockets(ctx) }()
	go func() { defer wg.Done(); alts, altNotes = DetectAlternatives(ctx) }()
	go func() { defer wg.Done(); vhosts = DetectVhosts() }()
	go func() { defer wg.Done(); mods = DetectApacheModules() }()
	go func() { defer wg.Done(); pathBin, pathVer = PathPHP(ctx) }()
	wg.Wait()

	s.Packages, s.Services, s.Sockets, s.Vhosts = pkgs, services, sockets, vhosts
	s.Alternatives = alts
	s.PathBinary, s.PathVersion = pathBin, pathVer

	notes := append([]string{}, pkgNotes...)
	notes = append(notes, phpNotes...)
	notes = append(notes, svcNotes...)
	notes = append(notes, sockNotes...)
	notes = append(notes, altNotes...)

	for _, alt := range alts {
		if alt.Name == "php" && alt.Current != "" {
			s.ActiveVersion = VersionFromBinaryPath(alt.Current)
			break
		}
	}
	if s.ActiveVersion == "" {
		s.ActiveVersion = VersionFromBinaryPath(alternativesDefault(alts))
	}
	if s.ActiveVersion == "" {
		s.ActiveVersion = pathVer
	}

	fallback := s.ActiveVersion
	if fallback == "" && len(versions) == 1 {
		fallback = versions[0].Raw
	}
	notes = AttachApacheModules(versions, mods, fallback, notes)
	notes = AttachServices(versions, services, fallback, notes)
	for i := range s.Services {
		if s.Services[i].Version == "" {
			s.Services[i].Version = fallback
		}
	}
	AttachSockets(versions, sockets)
	AttachPackages(versions, pkgs, s.PathVersion)

	for i := range versions {
		versions[i].Active = versions[i].Raw == s.ActiveVersion
		versions[i].InPath = versions[i].Raw == pathVer && versions[i].Raw != ""
	}
	s.Versions = versions

	available, n := DetectAvailable(ctx, s.Distro, versions)
	s.Available, notes = available, append(notes, n...)

	s.Capabilities = Capabilities(s.Distro, hasCommand("apt-cache"))
	s.Warnings = BuildWarnings(s)
	s.Notes = dedupeNotes(notes)

	if s.Versions == nil {
		s.Versions = []core.Version{}
	}
	if s.Packages == nil {
		s.Packages = []core.Package{}
	}
	if s.Services == nil {
		s.Services = []core.Service{}
	}
	if s.Sockets == nil {
		s.Sockets = []core.Socket{}
	}
	if s.Vhosts == nil {
		s.Vhosts = []core.Vhost{}
	}
	if s.Alternatives == nil {
		s.Alternatives = []core.Alternative{}
	}
	if s.Available == nil {
		s.Available = []core.AvailableVersion{}
	}
	if s.Warnings == nil {
		s.Warnings = []core.Warning{}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

func alternativesDefault(alts []core.Alternative) string {
	for _, a := range alts {
		if a.Name == "php" {
			return a.Current
		}
	}
	return ""
}

func dedupeNotes(notes []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(notes))
	for _, note := range notes {
		note = strings.TrimSpace(note)
		if note == "" || seen[note] {
			continue
		}
		seen[note] = true
		out = append(out, note)
	}
	return out
}

// PathPHP reports which php binary the current PATH resolves to.
func PathPHP(ctx context.Context) (binary, version string) {
	path, err := exec.LookPath("php")
	if err != nil {
		return "", ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		binary = resolved
	} else {
		binary = path
	}
	if out, err := run(ctx, 3*time.Second, path, "-v"); err == nil {
		major, minor, _, _, ok := ParseVersionOutput(out)
		if ok {
			return binary, fmt.Sprintf("%d.%d", major, minor)
		}
	}
	return binary, VersionFromBinaryPath(path)
}

// AttachPackages associates OS packages with the versions they belong to.
func AttachPackages(versions []core.Version, pkgs []core.Package, pathVersion string) {
	for i := range versions {
		versions[i].Packages = PackagesForVersion(pkgs, versions[i].Raw)
	}

	// Unversioned package names (Fedora, Arch, Alpine) belong to the version
	// that is currently first in PATH.
	fallback := pathVersion
	if fallback == "" && len(versions) == 1 {
		fallback = versions[0].Raw
	}
	if fallback == "" {
		return
	}
	for i := range versions {
		if versions[i].Raw != fallback {
			continue
		}
		known := map[string]bool{}
		for _, p := range versions[i].Packages {
			known[p.Name] = true
		}
		for _, p := range pkgs {
			if known[p.Name] || isVersionedPackage(p.Name) {
				continue
			}
			versions[i].Packages = append(versions[i].Packages, p)
			known[p.Name] = true
		}
		sort.Slice(versions[i].Packages, func(a, b int) bool {
			return versions[i].Packages[a].Name < versions[i].Packages[b].Name
		})
	}
}

func isVersionedPackage(name string) bool {
	rest, ok := strings.CutPrefix(name, "php")
	if !ok {
		return false
	}
	// php8.2-*, php82-*, php8.2
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	return digits > 0
}

// BuildWarnings derives diagnostics from the snapshot.
func BuildWarnings(s *core.Snapshot) []core.Warning {
	var out []core.Warning
	add := func(w core.Warning) { out = append(out, w) }

	if len(s.Versions) == 0 {
		add(core.Warning{
			Severity: core.SeverityWarn,
			Code:     "no-php",
			Message:  "Nenhuma instalação de PHP foi encontrada.",
			Hint:     "Instale pelo gerenciador de pacotes da distribuição. A instalação via phpman chega no M1.",
		})
	}
	if len(s.Versions) > 0 && s.ActiveVersion == "" {
		add(core.Warning{
			Severity: core.SeverityWarn,
			Code:     "no-default",
			Message:  "Existem versões instaladas, mas nenhuma está definida como padrão.",
			Hint:     "Defina a versão ativa (no M0 apenas informe): sudo update-alternatives --config php",
		})
	}
	if s.ActiveVersion != "" && s.PathVersion != "" && s.ActiveVersion != s.PathVersion {
		add(core.Warning{
			Severity: core.SeverityWarn,
			Code:     "path-mismatch",
			Message:  fmt.Sprintf("O PHP do PATH é %s, mas o padrão do sistema é %s.", s.PathVersion, s.ActiveVersion),
			Hint:     "O PATH do usuário pode ter precedência. Verifique ~/.local/bin e a ordem do PATH.",
			Details:  []string{"PATH: " + s.PathBinary},
		})
	}
	if s.ActiveVersion != "" && s.PathVersion == "" && len(s.Versions) > 0 {
		add(core.Warning{
			Severity: core.SeverityInfo,
			Code:     "php-not-in-path",
			Message:  "O binário php não está no PATH deste usuário.",
			Hint:     "Abra um novo shell ou ajuste o PATH.",
		})
	}

	for _, v := range s.Versions {
		fpm, hasFPM := v.SAPI(core.SAPIFPM)
		if hasFPM {
			switch {
			case fpm.Unit == "":
				add(core.Warning{
					Severity: core.SeverityInfo,
					Code:     "fpm-no-unit",
					Message:  fmt.Sprintf("PHP %s tem FPM instalado, mas nenhuma unidade systemd foi encontrada.", v.Raw),
					Hint:     "Verifique o pacote php-fpm da sua distribuição.",
				})
			case !fpm.Active:
				add(core.Warning{
					Severity: core.SeverityWarn,
					Code:     "fpm-inactive",
					Message:  fmt.Sprintf("O FPM do PHP %s está parado (%s).", v.Raw, fpm.Unit),
					Hint:     "sudo systemctl start " + fpm.Unit,
				})
			case fpm.Socket == "":
				add(core.Warning{
					Severity: core.SeverityInfo,
					Code:     "fpm-no-socket",
					Message:  fmt.Sprintf("FPM do PHP %s está ativo, mas nenhum socket foi localizado.", v.Raw),
					Hint:     "Confira listen= em /etc/php/" + v.Raw + "/fpm/pool.d/",
				})
			}
			if fpm.Socket != "" && !fpm.SocketExists && (fpm.Unit == "" || fpm.Active) {
				add(core.Warning{
					Severity: core.SeverityWarn,
					Code:     "socket-missing",
					Message:  fmt.Sprintf("O socket %s está configurado para o PHP %s, mas não existe.", fpm.Socket, v.Raw),
					Hint:     "Inicie o FPM ou verifique o pool. O serviço pode recriar o socket.",
				})
			}
		}
		if v.Supports(core.SAPIApache) && s.Distro.Family == core.FamilyDebian {
			add(core.Warning{
				Severity: core.SeverityInfo,
				Code:     "mod-php",
				Message:  fmt.Sprintf("O PHP %s está carregado como módulo do Apache (mod_php).", v.Raw),
				Hint:     "Trocar a versão exige a2dismod/a2enmod e reinício do Apache.",
			})
		}
	}

	byPath := map[string]core.Socket{}
	for _, sock := range s.Sockets {
		byPath[sock.Path] = sock
	}
	reported := map[string]bool{}
	for _, vh := range s.Vhosts {
		if vh.Kind != "fpm-socket" {
			continue
		}
		sock, known := byPath[vh.Target]
		if known && sock.Exists {
			continue
		}
		if reported[vh.Target] {
			continue
		}
		reported[vh.Target] = true
		code, msg, hint := "vhost-socket-unknown", "", ""
		if known {
			code = "vhost-socket-missing"
			msg = "Um site aponta para o socket " + vh.Target + ", que não existe."
			hint = "Inicie o serviço FPM correspondente ou corrija a configuração do site."
		} else {
			msg = "Um site usa o socket " + vh.Target + ", que não foi localizado na configuração de nenhum pool."
			hint = "Confira se o pool do FPM aponta para o mesmo caminho."
		}
		add(core.Warning{
			Severity: core.SeverityWarn,
			Code:     code,
			Message:  msg,
			Hint:     hint,
			Details:  []string{vh.File + ":" + itoaLine(vh.Line)},
		})
	}

	if len(s.Versions) > 1 {
		add(core.Warning{
			Severity: core.SeverityInfo,
			Code:     "multi-version",
			Message:  fmt.Sprintf("%d versões de PHP instaladas: %s.", len(s.Versions), versionList(s.Versions)),
			Hint:     "Mantenha apenas as versões realmente usadas para reduzir superfície de ataque.",
		})
	}

	if s.Distro.Family != core.FamilyDebian {
		add(core.Warning{
			Severity: core.SeverityInfo,
			Code:     "distro-unscoped",
			Message:  "Detecção genérica ativa; catálogo de versões e ações de escrita estão limitados a Debian/Ubuntu no MVP.",
			Hint:     "Nada foi alterado. O suporte a esta família entra nas próximas fases.",
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return severityRank(out[i].Severity) < severityRank(out[j].Severity)
	})
	return out
}

func severityRank(s core.Severity) int {
	switch s {
	case core.SeverityError:
		return 0
	case core.SeverityWarn:
		return 1
	default:
		return 2
	}
}

func versionList(versions []core.Version) string {
	parts := make([]string, 0, len(versions))
	for _, v := range versions {
		parts = append(parts, v.Raw)
	}
	return strings.Join(parts, ", ")
}

// Capabilities reports what phpman can do on this distribution. The caller
// supplies whether the package catalog backend is actually available so this
// function stays free of side effects.
func Capabilities(d core.Distro, catalogAvailable bool) []core.Capability {
	apt := d.Family == core.FamilyDebian
	catalog := apt && catalogAvailable
	caps := []core.Capability{
		{Key: "detect", Label: "Detectar instalações, SAPIs, serviços, sockets e sites", Supported: true, Detail: "somente leitura, sem root", Phase: "M0"},
		{Key: "doctor", Label: "Diagnóstico do ambiente PHP", Supported: true, Detail: "avisos acionáveis", Phase: "M0"},
		{Key: "plan", Label: "Simular instalação, remoção e troca", Supported: apt, Detail: "planos --dry-run, sem root", Phase: "M1"},
		{Key: "catalog", Label: "Catálogo de versões disponíveis", Supported: catalog, Detail: "consulta apt-cache", Phase: "M0"},
		{Key: "install", Label: "Instalar versões", Supported: false, Detail: "planejado", Phase: "M1"},
		{Key: "remove", Label: "Remover versões", Supported: false, Detail: "planejado", Phase: "M1"},
		{Key: "switch", Label: "Trocar a versão ativa", Supported: false, Detail: "planejado", Phase: "M1"},
		{Key: "extensions", Label: "Gerenciar extensões", Supported: false, Detail: "planejado", Phase: "M2"},
		{Key: "third-party", Label: "Adicionar repositórios de terceiros", Supported: false, Detail: "sury/ondrej com confirmação explícita", Phase: "M1"},
		{Key: "project-pin", Label: "Fixar versão por projeto", Supported: false, Detail: "fora do MVP", Phase: "—"},
	}
	for i := range caps {
		if caps[i].Key != "catalog" {
			continue
		}
		if !apt {
			caps[i].Detail = "família " + string(d.Family) + " ainda sem catálogo"
		} else if !catalog {
			caps[i].Detail = "apt-cache não encontrado"
		}
		break
	}
	return caps
}
