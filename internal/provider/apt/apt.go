// Package apt generates plans for Debian/Ubuntu systems. It is intentionally
// side-effect free: every method only reads the snapshot and returns data.
package apt

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/lucasarakaki/phpman/internal/core"
	"github.com/lucasarakaki/phpman/internal/provider"
)

var rawVersionRe = regexp.MustCompile(`^\d+\.\d+$`)

// Provider implements provider.Provider with apt-get and update-alternatives.
type Provider struct{}

// New returns an apt plan provider.
func New() *Provider { return &Provider{} }

// PlanInstall describes how to install one PHP X.Y version.
func (p *Provider) PlanInstall(snap *core.Snapshot, raw string, opts provider.Options) (*core.Plan, error) {
	if err := validate(snap, raw); err != nil {
		return nil, err
	}
	if snap.Version(raw) != nil {
		return newPlan(core.ActionInstall, raw, "PHP "+raw+" já está instalada", nil, nil,
			[]string{"Nenhuma alteração necessária."}), nil
	}

	av, ok := availableVersion(snap, raw)
	if !ok || strings.TrimSpace(av.Candidate) == "" {
		return nil, fmt.Errorf("%w: PHP %s não aparece no catálogo apt; repositórios de terceiros (sury/ondrej) serão oferecidos numa etapa separada", provider.ErrVersionUnavailable, raw)
	}

	packages := installPackages(raw, opts.WithFPM)
	unit := fpmUnit(raw)
	steps := []core.Step{{
		ID:          "install-packages",
		Description: "Instalar " + strings.Join(packages, ", "),
		Argv:        append([]string{"apt-get", "install", "-y", "--no-install-recommends"}, packages...),
		NeedsRoot:   true,
		Verify:      core.VerifyPackagesInstalled,
		VerifyArgs:  packages,
	}}

	if opts.WithFPM {
		steps = append(steps,
			core.Step{
				ID:          "enable-fpm",
				Description: "Habilitar e iniciar o FPM",
				Argv:        []string{"systemctl", "enable", "--now", unit},
				NeedsRoot:   true,
				Verify:      core.VerifyUnitActive,
				VerifyArgs:  []string{unit},
			},
			core.Step{
				ID:          "verify-fpm-enabled",
				Description: "Confirmar que o FPM está habilitado no boot",
				Verify:      core.VerifyUnitEnabled,
				VerifyArgs:  []string{unit},
			},
		)
	}

	steps = append(steps, core.Step{
		ID:          "verify-binary",
		Description: "Confirmar que o binário responde com a versão esperada",
		Verify:      core.VerifyBinaryVersion,
		VerifyArgs:  []string{"/usr/bin/php" + raw, raw},
	})
	if opts.WithFPM {
		steps = append(steps, core.Step{
			ID:          "verify-socket",
			Description: "Confirmar que o socket padrão do FPM foi criado",
			Verify:      core.VerifySocketExists,
			VerifyArgs:  []string{fpmSocket(raw)},
			Optional:    true,
		})
	}

	var warnings []string
	if av.Origin == "terceiros" {
		source := av.Source
		if source == "" {
			source = "repositório de terceiros"
		}
		warnings = append(warnings, "Os pacotes vêm de "+source+"; confirme a origem antes de executar o plano.")
	}
	if snap.ActiveVersion != "" && snap.ActiveVersion != raw {
		warnings = append(warnings, "A versão ativa continuará sendo PHP "+snap.ActiveVersion+"; use o plano de switch para ativar a nova versão.")
	}
	if snap.PathVersion != "" && snap.PathVersion != raw {
		warnings = append(warnings, "O PHP do PATH deste usuário é "+snap.PathVersion+" e pode ter precedência sobre a versão instalada.")
	}

	rollback := make([]core.Step, 0, 2)
	if opts.WithFPM {
		rollback = append(rollback, core.Step{
			ID:          "rollback-stop-fpm",
			Description: "Parar e desabilitar o FPM instalado",
			Argv:        []string{"systemctl", "disable", "--now", unit},
			NeedsRoot:   true,
			Verify:      core.VerifyUnitInactive,
			VerifyArgs:  []string{unit},
		})
	}
	rollback = append(rollback, core.Step{
		ID:          "rollback-remove-packages",
		Description: "Remover os pacotes instalados pelo plano",
		Argv:        append([]string{"apt-get", "purge", "-y"}, packages...),
		NeedsRoot:   true,
		Verify:      core.VerifyPackagesAbsent,
		VerifyArgs:  packages,
	})

	summary := "Instalar PHP " + raw + " (CLI e OPcache)"
	if opts.WithFPM {
		summary = "Instalar PHP " + raw + " (CLI, FPM e OPcache)"
	}
	return newPlan(core.ActionInstall, raw, summary, steps, rollback, warnings), nil
}

// PlanRemove describes how to purge one installed PHP X.Y version.
func (p *Provider) PlanRemove(snap *core.Snapshot, raw string) (*core.Plan, error) {
	if err := validate(snap, raw); err != nil {
		return nil, err
	}
	v := snap.Version(raw)
	if v == nil {
		return nil, fmt.Errorf("%w: PHP %s", provider.ErrVersionNotInstalled, raw)
	}
	if snap.ActiveVersion == raw {
		return nil, fmt.Errorf("%w: PHP %s; troque para outra versão antes de removê-la", provider.ErrActiveVersion, raw)
	}

	packages := removePackages(snap, raw)
	if len(packages) == 0 {
		return nil, fmt.Errorf("%w: PHP %s", provider.ErrNoPackages, raw)
	}

	var steps []core.Step
	if fpm, ok := v.SAPI(core.SAPIFPM); ok {
		unit := fpm.Unit
		if unit == "" {
			unit = fpmUnit(raw)
		}
		steps = append(steps, core.Step{
			ID:          "stop-fpm",
			Description: "Parar e desabilitar o FPM",
			Argv:        []string{"systemctl", "disable", "--now", unit},
			NeedsRoot:   true,
			Verify:      core.VerifyUnitInactive,
			VerifyArgs:  []string{unit},
		})
	}

	steps = append(steps, core.Step{
		ID:          "purge-packages",
		Description: "Remover pacotes e configuração de PHP " + raw,
		Argv:        append([]string{"apt-get", "purge", "-y"}, packages...),
		NeedsRoot:   true,
		Verify:      core.VerifyPackagesAbsent,
		VerifyArgs:  packages,
	})

	warnings := []string{}
	if refs := affectedVhosts(snap, raw); len(refs) > 0 {
		warnings = append(warnings, fmt.Sprintf("Sites que usam o socket do PHP %s serão afetados: %s", raw, strings.Join(refs, ", ")))
	}
	warnings = append(warnings, "apt-get purge remove a configuração da versão; o rollback reinstala os pacotes, mas não garante restaurar arquivos removidos.")

	rollback := []core.Step{{
		ID:          "rollback-install-packages",
		Description: "Reinstalar os pacotes removidos",
		Argv:        append([]string{"apt-get", "install", "-y", "--no-install-recommends"}, packages...),
		NeedsRoot:   true,
		Verify:      core.VerifyPackagesInstalled,
		VerifyArgs:  packages,
	}}

	return newPlan(core.ActionRemove, raw, "Remover PHP "+raw, steps, rollback, warnings), nil
}

// PlanSwitch describes how to point update-alternatives at one installed
// version. It deliberately does not rewrite vhosts or restart services.
func (p *Provider) PlanSwitch(snap *core.Snapshot, raw string) (*core.Plan, error) {
	if err := validate(snap, raw); err != nil {
		return nil, err
	}
	if snap.Version(raw) == nil {
		return nil, fmt.Errorf("%w: PHP %s", provider.ErrVersionNotInstalled, raw)
	}
	if snap.ActiveVersion == raw {
		return newPlan(core.ActionSwitch, raw, "PHP "+raw+" já é a versão ativa", nil, nil,
			[]string{"Nenhuma alteração necessária."}), nil
	}

	var steps, rollback []core.Step
	var warnings []string
	switchedPHP := false
	if len(snap.Alternatives) == 0 {
		warnings = append(warnings, "update-alternatives não retornou entradas; nenhuma troca foi planejada.")
	}
	for _, alt := range snap.Alternatives {
		path, ok := alternativePath(alt, raw)
		if !ok {
			warnings = append(warnings, "Não foi possível localizar um candidato "+raw+" para update-alternatives "+alt.Name+".")
			continue
		}
		if alt.Current == path {
			continue
		}
		steps = append(steps, core.Step{
			ID:          "switch-" + alt.Name,
			Description: "Apontar " + alt.Name + " para " + path,
			Argv:        []string{"update-alternatives", "--set", alt.Name, path},
			NeedsRoot:   true,
			Verify:      core.VerifyAlternativeCurrent,
			VerifyArgs:  []string{alt.Name, path},
		})
		if alt.Name == "php" {
			switchedPHP = true
		}
		if alt.Current != "" {
			rollback = append(rollback, core.Step{
				ID:          "rollback-" + alt.Name,
				Description: "Restaurar " + alt.Name + " para " + alt.Current,
				Argv:        []string{"update-alternatives", "--set", alt.Name, alt.Current},
				NeedsRoot:   true,
				Verify:      core.VerifyAlternativeCurrent,
				VerifyArgs:  []string{alt.Name, alt.Current},
			})
		}
	}

	if switchedPHP {
		steps = append(steps, core.Step{
			ID:          "verify-php",
			Description: "Confirmar que /usr/bin/php responde com a versão esperada",
			Verify:      core.VerifyBinaryVersion,
			VerifyArgs:  []string{"/usr/bin/php", raw},
		})
	}

	if target := snap.Version(raw); switchedPHP && target != nil {
		if fpm, ok := target.SAPI(core.SAPIFPM); ok {
			unit := fpm.Unit
			if unit == "" {
				unit = fpmUnit(raw)
			}
			steps = append(steps, core.Step{
				ID:          "restart-fpm",
				Description: "Reiniciar o FPM de PHP " + raw + " para aplicar a troca",
				Argv:        []string{"systemctl", "restart", unit},
				NeedsRoot:   true,
				Verify:      core.VerifyUnitActive,
				VerifyArgs:  []string{unit},
			})
			socket := fpm.Socket
			if socket == "" {
				socket = fpmSocket(raw)
			}
			steps = append(steps, core.Step{
				ID:          "verify-fpm-socket",
				Description: "Confirmar que o socket do FPM foi recriado",
				Verify:      core.VerifySocketExists,
				VerifyArgs:  []string{socket},
				Optional:    true,
			})
			if active, known := serviceActive(snap, unit); known && !active {
				rollback = append(rollback, core.Step{
					ID:          "rollback-stop-fpm",
					Description: "Parar o FPM iniciado pelo plano",
					Argv:        []string{"systemctl", "stop", unit},
					NeedsRoot:   true,
					Verify:      core.VerifyUnitInactive,
					VerifyArgs:  []string{unit},
				})
			}
			warnings = append(warnings, "O FPM de PHP "+raw+" será reiniciado; vhosts que apontam para o socket de outra versão não são reescritos.")
		} else {
			warnings = append(warnings, "PHP "+raw+" não tem unidade FPM detectada; nenhum serviço será reiniciado.")
		}
	}
	if len(steps) == 0 && len(warnings) == 0 {
		warnings = append(warnings, "Nenhum alternative precisava ser alterado.")
	}

	if snap.PathVersion != "" && snap.PathVersion != raw {
		warnings = append(warnings, "O PHP resolvido pelo PATH deste usuário é "+snap.PathVersion+"; update-alternatives pode não alterar este shell.")
	}
	if snap.ActiveVersion != "" {
		warnings = append(warnings, "Vhosts não são reescritos; apenas o FPM alvo é reiniciado.")
	}

	return newPlan(core.ActionSwitch, raw, "Trocar a versão ativa para PHP "+raw, steps, rollback, warnings), nil
}

func validate(snap *core.Snapshot, raw string) error {
	if snap == nil {
		return fmt.Errorf("inventário ausente")
	}
	if snap.Distro.Family != core.FamilyDebian {
		return fmt.Errorf("%w: %s", provider.ErrUnsupportedDistro, snap.Distro.Family)
	}
	if !rawVersionRe.MatchString(raw) {
		return fmt.Errorf("%w: %q", provider.ErrInvalidVersion, raw)
	}
	return nil
}

func availableVersion(snap *core.Snapshot, raw string) (core.AvailableVersion, bool) {
	for _, av := range snap.Available {
		if av.Raw == raw {
			return av, true
		}
	}
	return core.AvailableVersion{}, false
}

func installPackages(raw string, withFPM bool) []string {
	packages := []string{"php" + raw + "-cli", "php" + raw + "-common"}
	if withFPM {
		packages = append(packages, "php"+raw+"-fpm")
	}
	return append(packages, "php"+raw+"-opcache")
}

func removePackages(snap *core.Snapshot, raw string) []string {
	seen := map[string]bool{}
	var packages []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || !belongsToVersion(name, raw) {
			return
		}
		seen[name] = true
		packages = append(packages, name)
	}

	for _, pkg := range snap.Packages {
		if pkg.Manager != "" && pkg.Manager != "dpkg" {
			continue
		}
		add(pkg.Name)
	}
	if v := snap.Version(raw); v != nil {
		for _, pkg := range v.Packages {
			if pkg.Manager != "" && pkg.Manager != "dpkg" {
				continue
			}
			add(pkg.Name)
		}
	}
	sort.Strings(packages)
	return packages
}

func belongsToVersion(name, raw string) bool {
	compact := strings.ReplaceAll(raw, ".", "")
	if name == "php"+raw || name == "php"+compact {
		return true
	}
	for _, prefix := range []string{
		"php" + raw + "-",
		"php" + compact + "-",
		"libphp" + raw,
		"libphp" + compact,
		"libapache2-mod-php" + raw,
		"libapache2-mod-php" + compact,
	} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func affectedVhosts(snap *core.Snapshot, raw string) []string {
	sockets := map[string]bool{}
	if v := snap.Version(raw); v != nil {
		if fpm, ok := v.SAPI(core.SAPIFPM); ok && fpm.Socket != "" {
			sockets[fpm.Socket] = true
		}
	}
	for _, sock := range snap.Sockets {
		if sock.Version == raw && sock.Path != "" {
			sockets[sock.Path] = true
		}
	}
	if len(sockets) == 0 {
		return nil
	}

	seen := map[string]bool{}
	var refs []string
	for _, vh := range snap.Vhosts {
		if !sockets[vh.Target] {
			continue
		}
		ref := vh.File
		if vh.Line > 0 {
			ref += fmt.Sprintf(":%d", vh.Line)
		}
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

func alternativePath(alt core.Alternative, raw string) (string, bool) {
	for _, candidate := range alt.Candidates {
		base := filepath.Base(candidate.Path)
		if strings.HasPrefix(base, alt.Name) && strings.Contains(base, raw) {
			return candidate.Path, true
		}
	}
	return "", false
}

func fpmUnit(raw string) string   { return "php" + raw + "-fpm.service" }
func fpmSocket(raw string) string { return "/run/php/php" + raw + "-fpm.sock" }

func serviceActive(snap *core.Snapshot, unit string) (active, known bool) {
	for _, svc := range snap.Services {
		if svc.Unit == unit {
			return svc.Active, true
		}
	}
	return false, false
}

func newPlan(action core.Action, raw, summary string, steps, rollback []core.Step, warnings []string) *core.Plan {
	if steps == nil {
		steps = []core.Step{}
	}
	if warnings == nil {
		warnings = []string{}
	}
	plan := &core.Plan{
		Schema:   core.PlanSchema,
		Action:   action,
		Version:  raw,
		Summary:  summary,
		Warnings: warnings,
		Steps:    steps,
		Rollback: rollback,
	}
	plan.NeedsRoot = plan.NeedsPrivilege()
	return plan
}
