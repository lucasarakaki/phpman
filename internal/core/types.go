// Package core defines the domain model shared by detection, reporting and the
// terminal UI. It contains no side effects and no knowledge of any specific
// distribution: providers translate their world into these types.
package core

import "time"

// Schema is the version of the Snapshot JSON payload.
const Schema = 1

// Family groups distributions by the way they package PHP.
type Family string

const (
	FamilyDebian  Family = "debian"
	FamilyFedora  Family = "fedora"
	FamilyRHEL    Family = "rhel"
	FamilyArch    Family = "arch"
	FamilySUSE    Family = "suse"
	FamilyAlpine  Family = "alpine"
	FamilyUnknown Family = "unknown"
)

// Distro is the detected operating system.
type Distro struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	PrettyName     string   `json:"pretty_name"`
	VersionID      string   `json:"version_id"`
	Codename       string   `json:"codename,omitempty"`
	Like           []string `json:"like,omitempty"`
	Family         Family   `json:"family"`
	PackageManager string   `json:"package_manager,omitempty"`
}

// SAPIKind identifies a PHP server API.
type SAPIKind string

const (
	SAPICLI    SAPIKind = "cli"
	SAPIFPM    SAPIKind = "fpm"
	SAPICGI    SAPIKind = "cgi"
	SAPIDBG    SAPIKind = "phpdbg"
	SAPIApache SAPIKind = "apache"
)

// SAPI is one executable entry point of a PHP version plus its runtime state.
type SAPI struct {
	Kind         SAPIKind `json:"kind"`
	Binary       string   `json:"binary,omitempty"`
	Unit         string   `json:"unit,omitempty"`
	Active       bool     `json:"active"`
	Enabled      bool     `json:"enabled"`
	State        string   `json:"state,omitempty"`
	Socket       string   `json:"socket,omitempty"`
	SocketExists bool     `json:"socket_exists"`
	IniFile      string   `json:"ini_file,omitempty"`
	IniScanDir   string   `json:"ini_scan_dir,omitempty"`
	ConfigDir    string   `json:"config_dir,omitempty"`
}

// Extension is a PHP module as seen by one version. M0 only fills Loaded.
type Extension struct {
	Name      string `json:"name"`
	Loaded    bool   `json:"loaded"`
	Enabled   bool   `json:"enabled"`
	Installed bool   `json:"installed"`
	Package   string `json:"package,omitempty"`
}

// Version is one installed PHP installation (all SAPIs with the same X.Y).
type Version struct {
	Raw        string      `json:"raw"`
	Major      int         `json:"major"`
	Minor      int         `json:"minor"`
	Full       string      `json:"full,omitempty"`
	Path       string      `json:"path,omitempty"`
	SAPIs      []SAPI      `json:"sapis"`
	Extensions []Extension `json:"extensions,omitempty"`
	Packages   []Package   `json:"packages,omitempty"`
	Active     bool        `json:"active"`
	InPath     bool        `json:"in_path"`
}

// Label returns the short X.Y form.
func (v Version) Label() string { return v.Raw }

// Supports reports whether the version has a given SAPI.
func (v Version) Supports(k SAPIKind) bool {
	_, ok := v.SAPI(k)
	return ok
}

// SAPI returns the SAPI of the given kind, if present.
func (v Version) SAPI(k SAPIKind) (SAPI, bool) {
	for _, s := range v.SAPIs {
		if s.Kind == k {
			return s, true
		}
	}
	return SAPI{}, false
}

// Package is an installed or available OS package related to PHP.
type Package struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Manager string `json:"manager"`
}

// Service is a PHP related systemd unit.
type Service struct {
	Unit         string `json:"unit"`
	Version      string `json:"version,omitempty"`
	Active       bool   `json:"active"`
	Enabled      bool   `json:"enabled"`
	State        string `json:"state"`
	EnabledState string `json:"enabled_state,omitempty"`
}

// Socket is an FPM listening endpoint discovered on disk.
type Socket struct {
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
	Exists  bool   `json:"exists"`
}

// Vhost is a web server configuration that consumes PHP.
type Vhost struct {
	File   string `json:"file"`
	Server string `json:"server,omitempty"`
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
	Line   int    `json:"line,omitempty"`
}

// AvailableVersion is a version offered by the package manager.
type AvailableVersion struct {
	Raw       string `json:"raw"`
	Package   string `json:"package"`
	Candidate string `json:"candidate,omitempty"`
	Origin    string `json:"origin"`
	Source    string `json:"source,omitempty"`
	Installed bool   `json:"installed"`
}

// Alternative is one update-alternatives (or equivalent) entry.
type Alternative struct {
	Name       string                 `json:"name"`
	Link       string                 `json:"link,omitempty"`
	Current    string                 `json:"current,omitempty"`
	Best       string                 `json:"best,omitempty"`
	Status     string                 `json:"status,omitempty"`
	Candidates []AlternativeCandidate `json:"candidates,omitempty"`
}

// AlternativeCandidate is one path registered for an alternative.
type AlternativeCandidate struct {
	Path     string `json:"path"`
	Priority int    `json:"priority,omitempty"`
}

// Severity classifies a diagnostic finding.
type Severity string

const (
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

// Warning is a diagnostic finding produced by the doctor.
type Warning struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Hint     string   `json:"hint,omitempty"`
	Details  []string `json:"details,omitempty"`
}

// Capability states what phpman can do on the detected distribution.
type Capability struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Supported bool   `json:"supported"`
	Detail    string `json:"detail"`
	Phase     string `json:"phase,omitempty"`
}

// Snapshot is the complete read-only view of the machine.
type Snapshot struct {
	Schema        int                `json:"schema"`
	GeneratedAt   time.Time          `json:"generated_at"`
	Distro        Distro             `json:"distro"`
	ActiveVersion string             `json:"active_version,omitempty"`
	PathVersion   string             `json:"path_version,omitempty"`
	PathBinary    string             `json:"path_binary,omitempty"`
	Alternatives  []Alternative      `json:"alternatives,omitempty"`
	Versions      []Version          `json:"versions"`
	Available     []AvailableVersion `json:"available,omitempty"`
	Packages      []Package          `json:"packages,omitempty"`
	Services      []Service          `json:"services,omitempty"`
	Sockets       []Socket           `json:"sockets,omitempty"`
	Vhosts        []Vhost            `json:"vhosts,omitempty"`
	Warnings      []Warning          `json:"warnings,omitempty"`
	Capabilities  []Capability       `json:"capabilities"`
	Notes         []string           `json:"notes,omitempty"`
}

// Active returns the installed version marked as active, if any.
func (s *Snapshot) Active() *Version {
	for i := range s.Versions {
		if s.Versions[i].Active {
			return &s.Versions[i]
		}
	}
	return nil
}

// Version returns the installed version matching raw (e.g. "8.2").
func (s *Snapshot) Version(raw string) *Version {
	for i := range s.Versions {
		if s.Versions[i].Raw == raw {
			return &s.Versions[i]
		}
	}
	return nil
}
