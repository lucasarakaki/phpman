package detect

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lucasarakaki/phpman/internal/core"
)

var (
	nginxServerNameRe = regexp.MustCompile(`(?i)^\s*server_name\s+([^;]+);`)
	nginxFastcgiRe    = regexp.MustCompile(`(?i)^\s*fastcgi_pass\s+([^;]+);`)
	nginxProxyPassRe  = regexp.MustCompile(`(?i)^\s*proxy_pass\s+([^;]+);`)

	apacheServerNameRe = regexp.MustCompile(`(?i)^\s*ServerName\s+(\S+)`)
	apacheSetHandlerRe = regexp.MustCompile(`(?i)SetHandler\s+"?proxy:(?:unix:)?([^"\s|]+)`)
	apacheModPHPRe     = regexp.MustCompile(`(?i)SetHandler\s+application/x-httpd-php\b`)
	apacheProxyMatchRe = regexp.MustCompile(`(?i)ProxyPassMatch\s+\S+\s+(\S+)`)

	socketTargetRe = regexp.MustCompile(`(?i)(unix:)?(/[^\s;|"]+\.sock)`)
	tcpTargetRe    = regexp.MustCompile(`(?i)(?:https?://)?(127\.0\.0\.1|localhost|\[::1\]):(\d{2,5})`)
)

// DetectVhosts scans the common nginx and Apache configuration directories.
// Files that cannot be read (permissions) are silently skipped: M0 must work
// without root.
func DetectVhosts() []core.Vhost {
	var vhosts []core.Vhost
	seen := map[string]bool{}

	add := func(v core.Vhost) {
		key := v.File + "|" + v.Kind + "|" + v.Target + "|" + itoaLine(v.Line)
		if seen[key] {
			return
		}
		seen[key] = true
		vhosts = append(vhosts, v)
	}

	for _, path := range expandGlob("/etc/nginx/nginx.conf") {
		if data, err := os.ReadFile(path); err == nil {
			for _, v := range ParseNginxVhosts(path, string(data)) {
				add(v)
			}
		}
	}
	for _, root := range []string{"/etc/nginx/sites-enabled", "/etc/nginx/conf.d"} {
		walkConfigs(root, 4, func(path string) {
			if data, err := os.ReadFile(path); err == nil {
				for _, v := range ParseNginxVhosts(path, string(data)) {
					add(v)
				}
			}
		})
	}

	for _, root := range []string{"/etc/apache2/sites-enabled", "/etc/apache2/conf-enabled", "/etc/httpd/conf.d"} {
		walkConfigs(root, 3, func(path string) {
			if data, err := os.ReadFile(path); err == nil {
				for _, v := range ParseApacheVhosts(path, string(data)) {
					add(v)
				}
			}
		})
	}
	return vhosts
}

func itoaLine(n int) string {
	// small helper to keep add() allocation-free enough for a handful of files
	if n == 0 {
		return "0"
	}
	digits := [6]byte{}
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}

// walkConfigs visits regular files under root up to maxDepth. Unreadable
// directories end the walk for that branch.
func walkConfigs(root string, maxDepth int, fn func(string)) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.Count(rel, string(filepath.Separator)) >= maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".conf") && !strings.Contains(root, "sites-enabled") {
			return nil
		}
		fn(path)
		return nil
	})
}

// ParseNginxVhosts extracts PHP upstreams from an nginx configuration file.
func ParseNginxVhosts(path, content string) []core.Vhost {
	var out []core.Vhost
	sc := bufio.NewScanner(strings.NewReader(content))
	lineNo := 0
	serverName := ""
	depth := 0
	serverDepth := -1

	for sc.Scan() {
		lineNo++
		line := stripComment(sc.Text(), '#')
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if serverDepth < 0 && strings.Contains(trimmed, "server") && strings.Contains(trimmed, "{") &&
			!strings.HasPrefix(trimmed, "server_name") {
			serverDepth = depth
			serverName = ""
		}

		if m := nginxServerNameRe.FindStringSubmatch(line); m != nil && serverDepth >= 0 {
			serverName = strings.TrimSpace(m[1])
		}

		var target string
		if m := nginxFastcgiRe.FindStringSubmatch(line); m != nil {
			target = m[1]
		} else if m := nginxProxyPassRe.FindStringSubmatch(line); m != nil {
			target = m[1]
		}
		if target != "" {
			if norm, kind := NormalizeTarget(target); kind != "" {
				out = append(out, core.Vhost{File: path, Server: serverName, Kind: kind, Target: norm, Line: lineNo})
			}
		}

		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if serverDepth >= 0 && depth <= serverDepth {
			serverDepth = -1
			serverName = ""
		}
	}
	return out
}

// ParseApacheVhosts extracts PHP handlers from an Apache configuration file.
func ParseApacheVhosts(path, content string) []core.Vhost {
	var out []core.Vhost
	sc := bufio.NewScanner(strings.NewReader(content))
	lineNo := 0
	serverName := ""

	for sc.Scan() {
		lineNo++
		line := stripComment(sc.Text(), '#')
		if strings.TrimSpace(line) == "" {
			continue
		}
		if m := apacheServerNameRe.FindStringSubmatch(line); m != nil {
			serverName = m[1]
		}
		if apacheModPHPRe.MatchString(line) {
			out = append(out, core.Vhost{File: path, Server: serverName, Kind: "mod_php", Line: lineNo})
			continue
		}
		target := ""
		if m := apacheSetHandlerRe.FindStringSubmatch(line); m != nil {
			target = m[1]
		} else if m := apacheProxyMatchRe.FindStringSubmatch(line); m != nil {
			target = m[1]
		}
		if target == "" {
			continue
		}
		if norm, kind := NormalizeTarget(target); kind != "" {
			out = append(out, core.Vhost{File: path, Server: serverName, Kind: kind, Target: norm, Line: lineNo})
		}
	}
	return out
}

// NormalizeTarget canonicalises an upstream and classifies it. Only PHP
// upstreams are returned; static proxy_pass entries yield an empty kind.
func NormalizeTarget(target string) (string, string) {
	t := strings.TrimSpace(target)
	t = strings.Trim(t, `"'`)
	t = strings.TrimSuffix(t, "|")
	t = strings.TrimPrefix(t, "http://")
	t = strings.TrimPrefix(t, "https://")
	t = strings.TrimPrefix(t, "unix:")

	if m := socketTargetRe.FindStringSubmatch(t); m != nil {
		path := m[2]
		if isPHPUpstream(path) {
			return path, "fpm-socket"
		}
	}
	if m := tcpTargetRe.FindStringSubmatch(t); m != nil {
		if m[2] == "9000" || m[2] == "9001" || m[2] == "9070" || m[2] == "9080" {
			return m[1] + ":" + m[2], "fpm-tcp"
		}
	}
	return "", ""
}

// isPHPUpstream avoids reporting unrelated unix sockets (gunicorn, postgres,
// docker) as PHP-FPM endpoints.
func isPHPUpstream(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "php") || strings.Contains(lower, "fpm")
}

// stripComment removes an end-of-line comment introduced by marker.
func stripComment(line string, marker byte) string {
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case marker:
			if !inSingle && !inDouble {
				return line[:i]
			}
		}
	}
	return line
}
