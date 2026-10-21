package detect

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/lucasarakaki/phpman/internal/core"
)

// alternativeNames are the commands managed by update-alternatives that are
// relevant to PHP version switching.
var alternativeNames = []string{"php", "phpize", "php-config", "phpdbg", "phar"}

// DetectAlternatives reads update-alternatives state when available. On
// systems without it (Fedora, Arch, Alpine) the list is empty by design.
func DetectAlternatives(ctx context.Context) ([]core.Alternative, []string) {
	if !hasCommand("update-alternatives") {
		return nil, nil
	}

	var alts []core.Alternative
	for _, name := range alternativeNames {
		out, err := run(ctx, 3*time.Second, "update-alternatives", "--query", name)
		if err != nil {
			continue
		}
		if alt, ok := ParseAlternativesQuery(out); ok {
			alt.Name = name
			alts = append(alts, alt)
		}
	}
	return alts, nil
}

// ParseAlternativesQuery parses `update-alternatives --query php` output.
func ParseAlternativesQuery(out string) (core.Alternative, bool) {
	var alt core.Alternative
	found := false
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "Link":
			alt.Link = value
		case "Value":
			alt.Current = value
			found = true
		case "Best":
			alt.Best = value
		case "Status":
			alt.Status = value
		}
	}
	if alt.Name == "" && alt.Link != "" {
		parts := strings.Split(alt.Link, "/")
		alt.Name = parts[len(parts)-1]
	}
	return alt, found
}

// VersionFromBinaryPath extracts "8.2" from paths such as
// /usr/bin/php8.2 or /etc/alternatives/php -> /usr/bin/php8.2.
func VersionFromBinaryPath(path string) string {
	base := path
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	if m := binaryNameRe.FindStringSubmatch(base); m != nil && m[2] != "" {
		major, minor := splitRaw(m[2])
		if minor == 0 {
			return ""
		}
		return strconv.Itoa(major) + "." + strconv.Itoa(minor)
	}
	return ""
}
