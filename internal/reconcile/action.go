// Package reconcile computes deterministic operations and resumable receipts.
package reconcile

// ActionKind identifies one ordered, idempotent reconciliation effect.
type ActionKind string

const (
	ActionPrepareWorkspace     ActionKind = "prepare_workspace"
	ActionSyncContent          ActionKind = "sync_content"
	ActionSyncDatabase         ActionKind = "sync_database"
	ActionInstallToolchain     ActionKind = "install_toolchain"
	ActionConfigureApplication ActionKind = "configure_application"
	ActionVerifyState          ActionKind = "verify_state"
)

// Action is a stable operation descriptor. It contains no secret or timestamp.
type Action struct {
	ID         string     `json:"id"`
	Kind       ActionKind `json:"kind"`
	Idempotent bool       `json:"idempotent"`
}

func action(id string, kind ActionKind) Action {
	return Action{ID: id, Kind: kind, Idempotent: true}
}
