package core

// PlanSchema is the version of the Plan JSON payload. It is independent from
// Snapshot.Schema because plans and inventories evolve at different speeds.
const PlanSchema = 1

// Action is the kind of change described by a plan.
type Action string

const (
	ActionInstall Action = "install"
	ActionRemove  Action = "remove"
	ActionSwitch  Action = "switch"
)

// VerifyKind identifies a declarative post-step check. It is a string instead
// of a function so that a plan can be serialized, compared and tested as data.
type VerifyKind string

const (
	VerifyPackagesInstalled  VerifyKind = "packages-installed"
	VerifyPackagesAbsent     VerifyKind = "packages-absent"
	VerifyBinaryVersion      VerifyKind = "binary-version"
	VerifyUnitActive         VerifyKind = "unit-active"
	VerifyUnitInactive       VerifyKind = "unit-inactive"
	VerifyUnitEnabled        VerifyKind = "unit-enabled"
	VerifySocketExists       VerifyKind = "socket-exists"
	VerifyAlternativeCurrent VerifyKind = "alternative-current"
)

// Step is one planned action. Argv is already split and is never passed through
// a shell. Verification-only steps have an empty Argv and a Verify kind.
type Step struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	Argv        []string   `json:"argv,omitempty"`
	NeedsRoot   bool       `json:"needs_root"`
	Verify      VerifyKind `json:"verify,omitempty"`
	VerifyArgs  []string   `json:"verify_args,omitempty"`
	Optional    bool       `json:"optional,omitempty"`
}

// Plan is the complete, serializable description of a change. It never
// contains timestamps or other volatile data, so equal inputs produce equal
// plans.
type Plan struct {
	Schema    int      `json:"schema"`
	Action    Action   `json:"action"`
	Version   string   `json:"version"`
	Summary   string   `json:"summary"`
	NeedsRoot bool     `json:"needs_root"`
	Warnings  []string `json:"warnings,omitempty"`
	Steps     []Step   `json:"steps"`
	Rollback  []Step   `json:"rollback,omitempty"`
}

// NeedsPrivilege reports whether any step or rollback step requires root.
func (p *Plan) NeedsPrivilege() bool {
	for _, s := range p.Steps {
		if s.NeedsRoot {
			return true
		}
	}
	for _, s := range p.Rollback {
		if s.NeedsRoot {
			return true
		}
	}
	return false
}
