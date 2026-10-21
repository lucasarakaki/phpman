package detect

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucasarakaki/phpman/internal/core"
)

// phpSearchDirs are the locations where distributions install PHP binaries.
var phpSearchDirs = []string{
	"/usr/bin",
	"/usr/sbin",
	"/usr/local/bin",
	"/usr/local/sbin",
}

var (
	// php8.2, php-fpm8.2, php-cgi8.2, phpdbg8.2, php, php-fpm
	binaryNameRe = regexp.MustCompile(`^php(?:-?(fpm|cgi|dbg))?(\d+(?:\.\d+)?)?$`)
	// "PHP 8.5.11 (cli) (built: ...)" and "phpdbg 8.2.29 (cli)".
	versionOutputRe = regexp.MustCompile(`(?i)\bphp\s+(\d+)\.(\d+)\.(\d+)[^(]*\(([^)]+)\)`)
	// unit names such as php8.2-fpm.service or php-fpm.service.
	unitVersionRe = regexp.MustCompile(`php(\d+)\.(\d+)-fpm`)
	libphpRe      = regexp.MustCompile(`libphp(\d+)\.(\d+)\.so`)
	socketNameRe  = regexp.MustCompile(`php(\d+)\.(\d+)-fpm`)
)

type phpBinary struct {
	Path     string
	Name     string
	Kind     core.SAPIKind
	NamedRaw string
	NamedMJ  int
	NamedMN  int
}

// DiscoverBinaries walks the standard binary directories and returns unique
// PHP executables. Symlinks are resolved so "php" and "php8.2" collapse into a
// single installation.
func DiscoverBinaries(dirs []string) []phpBinary {
	seen := map[string]bool{}
	var out []phpBinary

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			m := binaryNameRe.FindStringSubmatch(name)
			if m == nil {
				continue
			}
			path := filepath.Join(dir, name)
			if !isExecutable(path) {
				continue
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				continue
			}
			if seen[resolved] {
				continue
			}
			seen[resolved] = true

			b := phpBinary{Path: resolved, Name: name, Kind: core.SAPICLI}
			switch m[1] {
			case "fpm":
				b.Kind = core.SAPIFPM
			case "cgi":
				b.Kind = core.SAPICGI
			case "dbg":
				b.Kind = core.SAPIDBG
			}
			b.NamedRaw = m[2]
			if raw := b.NamedRaw; raw != "" {
				b.NamedMJ, b.NamedMN = splitRaw(raw)
			}
			out = append(out, b)
		}
	}
	return out
}

// splitRaw turns "8.2" into (8, 2). A bare "8" yields (8, 0).
func splitRaw(raw string) (int, int) {
	parts := strings.SplitN(raw, ".", 2)
	major, _ := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	return major, minor
}

// ParseVersionOutput extracts the PHP version and SAPI from `php -v` output.
func ParseVersionOutput(out string) (major, minor, patch int, sapi string, ok bool) {
	for _, line := range strings.Split(out, "\n") {
		m := versionOutputRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		major, _ = strconv.Atoi(m[1])
		minor, _ = strconv.Atoi(m[2])
		patch, _ = strconv.Atoi(m[3])
		return major, minor, patch, strings.ToLower(strings.TrimSpace(m[4])), true
	}
	return 0, 0, 0, "", false
}

// kindFromSAPI maps the SAPI name printed by PHP to a SAPIKind.
func kindFromSAPI(name string) core.SAPIKind {
	switch {
	case strings.Contains(name, "fpm"):
		return core.SAPIFPM
	case strings.Contains(name, "cgi"):
		return core.SAPICGI
	case strings.Contains(name, "dbg"):
		return core.SAPIDBG
	default:
		return core.SAPICLI
	}
}

type probeResult struct {
	binary phpBinary
	major  int
	minor  int
	patch  int
	sapi   string
	ok     bool
}

// DetectPHP inventories every installed PHP version and its SAPIs.
func DetectPHP(ctx context.Context) ([]core.Version, []string) {
	var notes []string
	binaries := DiscoverBinaries(phpSearchDirs)
	if len(binaries) == 0 {
		return nil, []string{"nenhum executável PHP encontrado em /usr/bin, /usr/sbin, /usr/local/bin ou /usr/local/sbin"}
	}

	results := make([]probeResult, len(binaries))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, b := range binaries {
		wg.Add(1)
		go func(i int, b phpBinary) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			out, _ := run(ctx, 3*time.Second, b.Path, "-v")
			major, minor, patch, sapi, ok := ParseVersionOutput(out)
			results[i] = probeResult{binary: b, major: major, minor: minor, patch: patch, sapi: sapi, ok: ok}
		}(i, b)
	}
	wg.Wait()

	buckets := map[string]*core.Version{}
	for _, r := range results {
		major, minor := r.major, r.minor
		patch := r.patch
		if !r.ok {
			if r.binary.NamedMJ == 0 {
				notes = appendNote(notes, "não foi possível determinar a versão de "+r.binary.Path)
				continue
			}
			major, minor = r.binary.NamedMJ, r.binary.NamedMN
			patch = 0
			notes = appendNote(notes, "versão de "+r.binary.Path+" inferida pelo nome do arquivo")
		}
		raw := strconv.Itoa(major) + "." + strconv.Itoa(minor)
		v, ok := buckets[raw]
		if !ok {
			v = &core.Version{Raw: raw, Major: major, Minor: minor}
			buckets[raw] = v
		}
		if patch > 0 {
			v.Full = strconv.Itoa(major) + "." + strconv.Itoa(minor) + "." + strconv.Itoa(patch)
		}

		kind := r.binary.Kind
		if kind == "" {
			kind = core.SAPICLI
		}
		// When the file name is generic ("php", "php-fpm"), trust the SAPI
		// reported by the binary itself.
		if kind == core.SAPICLI && r.ok {
			kind = kindFromSAPI(r.sapi)
		}

		if existing, found := v.SAPI(kind); found {
			if preferBinary(existing.Binary, r.binary.Path) {
				existing.Binary = r.binary.Path
				replaceSAPI(v, existing)
			}
			continue
		}
		v.SAPIs = append(v.SAPIs, core.SAPI{Kind: kind, Binary: r.binary.Path})
	}

	versions := make([]core.Version, 0, len(buckets))
	for _, v := range buckets {
		sort.Slice(v.SAPIs, func(i, j int) bool { return sapiOrder(v.SAPIs[i].Kind) < sapiOrder(v.SAPIs[j].Kind) })
		if cli, ok := v.SAPI(core.SAPICLI); ok {
			v.Path = cli.Binary
			loadRuntimeInfo(ctx, v, cli.Binary)
		} else {
			for _, s := range v.SAPIs {
				if s.Binary != "" {
					v.Path = s.Binary
					break
				}
			}
		}
		loadConfigDirs(v)
		versions = append(versions, *v)
	}
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].Major != versions[j].Major {
			return versions[i].Major > versions[j].Major
		}
		return versions[i].Minor > versions[j].Minor
	})
	return versions, notes
}

// replaceSAPI swaps an existing SAPI entry, preserving order.
func replaceSAPI(v *core.Version, s core.SAPI) {
	for i := range v.SAPIs {
		if v.SAPIs[i].Kind == s.Kind {
			v.SAPIs[i] = s
			return
		}
	}
}

func sapiOrder(k core.SAPIKind) int {
	switch k {
	case core.SAPICLI:
		return 0
	case core.SAPIFPM:
		return 1
	case core.SAPIApache:
		return 2
	case core.SAPICGI:
		return 3
	case core.SAPIDBG:
		return 4
	default:
		return 9
	}
}

// preferBinary decides which binary wins when the same SAPI is found twice.
// Distribution paths beat locally compiled ones.
func preferBinary(current, candidate string) bool {
	if current == "" {
		return true
	}
	curLocal := strings.Contains(current, "/local/")
	newLocal := strings.Contains(candidate, "/local/")
	if curLocal != newLocal {
		return curLocal && !newLocal
	}
	return len(candidate) < len(current)
}

// loadRuntimeInfo fills extensions, php.ini and scan dir from the CLI binary.
func loadRuntimeInfo(ctx context.Context, v *core.Version, cli string) {
	if out, err := run(ctx, 3*time.Second, cli, "-m"); err == nil {
		v.Extensions = ParseModules(out)
	}
	if out, err := run(ctx, 3*time.Second, cli, "--ini"); err == nil {
		file, scanDir, _ := ParseIni(out)
		for i := range v.SAPIs {
			if v.SAPIs[i].Kind == core.SAPICLI {
				v.SAPIs[i].IniFile = file
				v.SAPIs[i].IniScanDir = scanDir
			}
		}
	}
}

// ParseModules extracts module names from `php -m`.
func ParseModules(out string) []core.Extension {
	var exts []core.Extension
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") || seen[line] {
			continue
		}
		seen[line] = true
		exts = append(exts, core.Extension{Name: line, Loaded: true, Enabled: true, Installed: true})
	}
	sort.Slice(exts, func(i, j int) bool {
		return strings.ToLower(exts[i].Name) < strings.ToLower(exts[j].Name)
	})
	return exts
}

// ParseIni extracts the loaded ini file and scan directory from `php --ini`.
// The list of additional ini files may span multiple lines.
func ParseIni(out string) (file, scanDir string, extra []string) {
	collecting := false
	addExtra := func(value string) {
		for _, p := range strings.Split(value, ",") {
			if p = strings.TrimSpace(p); p != "" && p != "(none)" {
				extra = append(extra, p)
			}
		}
	}

	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			switch key {
			case "Loaded Configuration File":
				collecting = false
				if value != "(none)" {
					file = unquote(value)
				}
			case "Scan for additional .ini files in":
				collecting = false
				if value != "(none)" {
					scanDir = unquote(value)
				}
			case "Additional .ini files parsed":
				collecting = true
				addExtra(value)
			default:
				collecting = false
			}
			continue
		}
		if collecting && strings.HasPrefix(line, "/") {
			addExtra(line)
		} else {
			collecting = false
		}
	}
	return file, scanDir, extra
}

// loadConfigDirs adds per-SAPI config directories for the Debian layout.
func loadConfigDirs(v *core.Version) {
	for i := range v.SAPIs {
		s := &v.SAPIs[i]
		base := filepath.Join("/etc/php", v.Raw, string(s.Kind))
		if s.Kind == core.SAPICLI && s.IniScanDir != "" {
			s.ConfigDir = filepath.Dir(s.IniScanDir)
			continue
		}
		if exists(base) {
			s.ConfigDir = base
			if s.IniFile == "" {
				ini := filepath.Join(base, "php.ini")
				if exists(ini) {
					s.IniFile = ini
				}
			}
			if s.IniScanDir == "" {
				confd := filepath.Join(base, "conf.d")
				if exists(confd) {
					s.IniScanDir = confd
				}
			}
		}
	}
}

// ApacheModule is a PHP module loaded by Apache.
type ApacheModule struct {
	Binary string
	Raw    string
	Source string
}

// DetectApacheModules inspects the common Apache configuration layouts.
func DetectApacheModules() []ApacheModule {
	var mods []ApacheModule
	seen := map[string]bool{}

	add := func(binary, path string) {
		raw := ""
		if m := libphpRe.FindStringSubmatch(filepath.Base(binary)); m != nil {
			raw = m[1] + "." + m[2]
		}
		key := binary + "|" + raw
		if seen[key] {
			return
		}
		seen[key] = true
		mods = append(mods, ApacheModule{Binary: binary, Raw: raw, Source: path})
	}

	for _, path := range expandGlob("/etc/apache2/mods-enabled/php*.load") {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "LoadModule") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				add(fields[2], path)
			}
		}
	}
	// Fedora/RHEL: /etc/httpd/conf.modules.d/20-php.conf or conf.d/php.conf.
	for _, pattern := range []string{"/etc/httpd/conf.modules.d/*php*.conf", "/etc/httpd/conf.d/*php*.conf", "/etc/httpd/conf.d/php.conf"} {
		for _, path := range expandGlob(pattern) {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if !strings.HasPrefix(line, "LoadModule") {
					continue
				}
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					add(fields[2], path)
				}
			}
		}
	}
	return mods
}

// AttachApacheModules adds Apache SAPIs to the matching installed versions.
// Versionless modules (libphp.so) are attached to fallbackRaw.
func AttachApacheModules(versions []core.Version, mods []ApacheModule, fallbackRaw string, notes []string) []string {
	for _, m := range mods {
		raw := m.Raw
		if raw == "" {
			raw = fallbackRaw
		}
		if raw == "" {
			notes = appendNote(notes, "módulo PHP do Apache encontrado sem versão identificável: "+m.Binary)
			continue
		}
		found := false
		for i := range versions {
			if versions[i].Raw != raw {
				continue
			}
			found = true
			if _, ok := versions[i].SAPI(core.SAPIApache); ok {
				break
			}
			sapi := core.SAPI{
				Kind:      core.SAPIApache,
				Binary:    m.Binary,
				ConfigDir: filepath.Join("/etc/php", raw, "apache2"),
				State:     "mod_php",
			}
			if ini := filepath.Join(sapi.ConfigDir, "php.ini"); exists(ini) {
				sapi.IniFile = ini
			}
			versions[i].SAPIs = append(versions[i].SAPIs, sapi)
			break
		}
		if !found {
			notes = appendNote(notes, "módulo Apache "+m.Binary+" sem versão PHP instalada correspondente")
		}
	}
	return notes
}
