package executor

import "github.com/lucasarakaki/phpman/internal/core"

// The helper protocol is JSON Lines over stdin/stdout. Requests carry a plan
// once and then reference its step IDs; events stream command output back to
// the client. No command output is ever mixed with the protocol because the
// helper re-encodes each line as a JSON event.

const (
	opPlan = "plan"
	opExec = "exec"
	opDone = "done"

	kindReady  = "ready"
	kindStdout = "stdout"
	kindStderr = "stderr"
	kindExit   = "exit"
	kindError  = "error"
)

type request struct {
	Op   string     `json:"op"`
	Plan *core.Plan `json:"plan,omitempty"`
	ID   string     `json:"id,omitempty"`
	Argv []string   `json:"argv,omitempty"`
}

type event struct {
	Kind  string `json:"kind"`
	ID    string `json:"id,omitempty"`
	Line  string `json:"line,omitempty"`
	Exit  *int   `json:"exit,omitempty"`
	Error string `json:"error,omitempty"`
}
