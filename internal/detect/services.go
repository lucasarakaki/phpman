package detect

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lucasarakaki/phpman/internal/core"
)

// DetectServices lists PHP-FPM units and their state via systemd. Unit files
// are discovered on disk first: `systemctl list-unit-files` enumerates every
// service and takes over a second on typical systems.
func DetectServices(ctx context.Context) ([]core.Service, []string) {
	var notes []string
	if !hasCommand("systemctl") {
		return nil, []string{"systemctl não encontrado; estado dos serviços indisponível"}
	}

	seen := map[string]bool{}
	var units []string
	for _, pattern := range []string{
		"/usr/lib/systemd/system/php*fpm*.service",
		"/lib/systemd/system/php*fpm*.service",
		"/etc/systemd/system/php*fpm*.service",
		"/run/systemd/system/php*fpm*.service",
	} {
		for _, path := range expandGlob(pattern) {
			name := filepath.Base(path)
			if strings.Contains(name, "@") || seen[name] {
				continue
			}
			seen[name] = true
			units = append(units, name)
		}
	}
	if len(units) == 0 {
		return nil, notes
	}
	sort.Strings(units)

	var services []core.Service
	for _, unit := range units {
		out, err := run(ctx, 2*time.Second, "systemctl", "show", unit,
			"--property=ActiveState,UnitFileState", "--no-pager")
		if strings.Contains(out, "not been booted with systemd") || strings.Contains(out, "Failed to connect to bus") {
			notes = appendNote(notes, "systemd não está em execução; estado dos serviços indisponível")
			return services, notes
		}
		if err != nil && out == "" {
			continue
		}

		svc := core.Service{Unit: unit, State: "unknown"}
		if m := unitVersionRe.FindStringSubmatch(unit); m != nil {
			svc.Version = m[1] + "." + m[2]
		}
		for _, line := range strings.Split(out, "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok {
				continue
			}
			switch key {
			case "ActiveState":
				svc.State = value
			case "UnitFileState":
				svc.EnabledState = value
			}
		}
		svc.Active = svc.State == "active"
		svc.Enabled = svc.EnabledState == "enabled" || svc.EnabledState == "static" || svc.EnabledState == "indirect"
		services = append(services, svc)
	}

	sort.Slice(services, func(i, j int) bool {
		if services[i].Version != services[j].Version {
			return services[i].Version > services[j].Version
		}
		return services[i].Unit < services[j].Unit
	})
	return services, notes
}

// AttachServices binds units to the installed versions they belong to.
// A versionless unit (php-fpm.service) belongs to fallbackRaw.
func AttachServices(versions []core.Version, services []core.Service, fallbackRaw string, notes []string) []string {
	for _, svc := range services {
		raw := svc.Version
		if raw == "" {
			raw = fallbackRaw
		}
		if raw == "" {
			notes = appendNote(notes, "unidade "+svc.Unit+" sem versão identificável")
			continue
		}
		for i := range versions {
			if versions[i].Raw != raw {
				continue
			}
			for j := range versions[i].SAPIs {
				s := &versions[i].SAPIs[j]
				if s.Kind != core.SAPIFPM || s.Unit != "" {
					continue
				}
				s.Unit = svc.Unit
				s.Active = svc.Active
				s.Enabled = svc.Enabled
				s.State = svc.State
			}
		}
	}
	return notes
}

// liveSocketPatterns are runtime directories used by the major distributions.
var liveSocketPatterns = []string{
	"/run/php/*.sock",
	"/run/php-fpm/*.sock",
	"/var/run/php/*.sock",
	"/var/run/php-fpm/*.sock",
}

var (
	listenRe = regexp.MustCompile(`(?mi)^\s*listen\s*=\s*([^;\s]+)`)
)

// ParsePoolListen extracts listen values from an FPM pool file.
func ParsePoolListen(content string) []string {
	var out []string
	for _, m := range listenRe.FindAllStringSubmatch(content, -1) {
		value := strings.Trim(strings.TrimSpace(m[1]), `"'`)
		if value == "" || strings.HasPrefix(value, "$") {
			continue
		}
		out = append(out, value)
	}
	return out
}

// DetectSockets finds FPM endpoints configured on disk and live in /run.
func DetectSockets(ctx context.Context) ([]core.Socket, []string) {
	var notes []string
	seen := map[string]*core.Socket{}
	var order []string

	add := func(path, version, source string) {
		path = strings.TrimPrefix(strings.TrimSpace(path), "unix:")
		path = strings.TrimSuffix(path, "|")
		if path == "" || !strings.HasPrefix(path, "/") {
			return
		}
		if s, ok := seen[path]; ok {
			if s.Version == "" && version != "" {
				s.Version = version
			}
			return
		}
		seen[path] = &core.Socket{Path: path, Version: version, Source: source, Exists: exists(path)}
		order = append(order, path)
	}

	poolPatterns := []struct {
		glob string
		// versionFromPath selects the /etc/php/<version>/fpm/... layout.
		debian bool
	}{
		{"/etc/php/*/fpm/pool.d/*.conf", true},
		{"/etc/php-fpm.d/*.conf", false},
	}
	for _, p := range poolPatterns {
		for _, file := range expandGlob(p.glob) {
			data, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			version := ""
			if p.debian {
				parts := strings.Split(file, "/")
				for i, part := range parts {
					if part == "php" && i+1 < len(parts) {
						version = parts[i+1]
						break
					}
				}
			}
			for _, listen := range ParsePoolListen(string(data)) {
				add(listen, version, file)
			}
		}
	}

	for _, pattern := range liveSocketPatterns {
		for _, path := range expandGlob(pattern) {
			version := ""
			if m := socketNameRe.FindStringSubmatch(path); m != nil {
				version = m[1] + "." + m[2]
			}
			add(path, version, "runtime")
		}
	}

	sockets := make([]core.Socket, 0, len(order))
	for _, path := range order {
		sockets = append(sockets, *seen[path])
	}
	sort.Slice(sockets, func(i, j int) bool {
		if sockets[i].Version != sockets[j].Version {
			return sockets[i].Version > sockets[j].Version
		}
		return sockets[i].Path < sockets[j].Path
	})
	return sockets, notes
}

// AttachSockets binds endpoints to the FPM SAPI of each version.
func AttachSockets(versions []core.Version, sockets []core.Socket) {
	for i := range versions {
		var match *core.Socket
		for j := range sockets {
			if sockets[j].Version == versions[i].Raw {
				match = &sockets[j]
				break
			}
		}
		if match == nil && len(versions) == 1 {
			for j := range sockets {
				if sockets[j].Version == "" {
					match = &sockets[j]
					sockets[j].Version = versions[i].Raw
					break
				}
			}
		}
		if match == nil {
			continue
		}
		for j := range versions[i].SAPIs {
			s := &versions[i].SAPIs[j]
			if s.Kind != core.SAPIFPM {
				continue
			}
			s.Socket = match.Path
			s.SocketExists = match.Exists
		}
	}
}
