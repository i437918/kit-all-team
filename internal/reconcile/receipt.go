package reconcile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/strictjson"
)

// EffectStatus records one resumable effect checkpoint.
type EffectStatus string

const (
	EffectPending   EffectStatus = "pending"
	EffectSucceeded EffectStatus = "succeeded"
	EffectFailed    EffectStatus = "failed"
)

// Checkpoint records the non-secret outcome of one planned action.
type Checkpoint struct {
	ActionID   string       `json:"action_id"`
	Status     EffectStatus `json:"status"`
	Diagnostic string       `json:"diagnostic,omitempty"`
}

type desiredSnapshot struct {
	OS            domain.OSFamily      `json:"os"`
	Application   domain.AIApplication `json:"application"`
	AppInstalled  bool                 `json:"app_installed"`
	KitHome       string               `json:"kit_home"`
	HermesHome    string               `json:"hermes_home,omitempty"`
	HermesVersion string               `json:"hermes_version,omitempty"`
	Project       domain.ProjectID     `json:"project"`
	Role          domain.Role          `json:"role"`
	Toolchain     domain.Toolchain     `json:"toolchain"`
}

type receiptJSON struct {
	SchemaVersion int             `json:"schema_version"`
	StateVersion  string          `json:"state_version"`
	PlanHash      string          `json:"plan_hash"`
	Desired       desiredSnapshot `json:"desired"`
	Checkpoints   []Checkpoint    `json:"checkpoints"`
}

// Receipt is a deterministic, incrementally checkpointed reconciliation record.
// Secret values supplied at construction are retained only for output redaction.
type Receipt struct {
	schemaVersion int
	stateVersion  string
	planHash      string
	desired       desiredSnapshot
	checkpoints   []Checkpoint
	redactions    []string
}

// NewReceipt creates pending checkpoints for every action in plan.
func NewReceipt(desired domain.DesiredState, plan OperationPlan, secrets ...string) *Receipt {
	snapshot := snapshotOf(desired)
	checkpoints := make([]Checkpoint, len(plan.Actions))
	for i, plannedAction := range plan.Actions {
		checkpoints[i] = Checkpoint{ActionID: plannedAction.ID, Status: EffectPending}
	}
	return &Receipt{
		schemaVersion: 1,
		stateVersion:  hashJSON(snapshot),
		planHash:      hashJSON(plan),
		desired:       snapshot,
		checkpoints:   checkpoints,
		redactions:    nonemptyStrings(secrets),
	}
}

// ParseReceipt strictly validates a complete receipt for later retry. Unknown
// fields, truncated JSON, invalid selectors, hashes, or checkpoint sequences
// are rejected rather than partially accepted.
func ParseReceipt(data []byte, secrets ...string) (*Receipt, error) {
	var receipt Receipt
	if err := receipt.UnmarshalJSON(data); err != nil {
		return nil, err
	}
	redactions := nonemptyStrings(secrets)
	for i := range receipt.checkpoints {
		receipt.checkpoints[i].Diagnostic = redact(receipt.checkpoints[i].Diagnostic, redactions)
	}
	receipt.redactions = redactions
	return &receipt, nil
}

// Record atomically replaces the first incomplete checkpoint outcome. A failed
// checkpoint remains current and must succeed before a later action can advance.
func (r *Receipt) Record(actionID string, status EffectStatus, diagnostic string) error {
	if status != EffectSucceeded && status != EffectFailed {
		return fmt.Errorf("CHECKPOINT_STATUS_INVALID: %q", status)
	}
	index := r.firstIncomplete()
	if index == -1 || r.checkpoints[index].ActionID != actionID {
		return fmt.Errorf("CHECKPOINT_OUT_OF_ORDER: %q", actionID)
	}
	r.checkpoints[index] = Checkpoint{
		ActionID:   actionID,
		Status:     status,
		Diagnostic: redact(diagnostic, r.redactions),
	}
	return nil
}

// Checkpoints returns a defensive copy of current effect outcomes.
func (r *Receipt) Checkpoints() []Checkpoint {
	return append([]Checkpoint(nil), r.checkpoints...)
}

// MatchesDesired proves that a retry request is for the exact public state
// captured when this receipt was created.
func (r *Receipt) MatchesDesired(desired domain.DesiredState) bool {
	return r != nil && r.stateVersion == hashJSON(snapshotOf(desired)) && r.desired == snapshotOf(desired)
}

// DesiredState reconstructs the validated public selector set captured by the
// receipt. It lets recovery start from an atomic operation envelope when the
// separately published workspace .env was not reached before interruption.
func (r *Receipt) DesiredState() (domain.DesiredState, error) {
	if r == nil {
		return domain.DesiredState{}, fmt.Errorf("RECEIPT_INVALID: missing desired state")
	}
	return domain.NewDesiredState(domain.DesiredStateInput{
		OS:            r.desired.OS,
		Application:   r.desired.Application,
		AppInstalled:  r.desired.AppInstalled,
		KitHome:       r.desired.KitHome,
		HermesHome:    r.desired.HermesHome,
		HermesVersion: r.desired.HermesVersion,
		Project:       r.desired.Project,
		Role:          r.desired.Role,
		Toolchain:     r.desired.Toolchain,
	})
}

// UnmarshalJSON strictly restores a receipt without accepting duplicate or
// unknown fields. ParseReceipt adds invocation-specific redactions afterward.
func (r *Receipt) UnmarshalJSON(data []byte) error {
	var decoded receiptJSON
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("RECEIPT_INVALID: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return err
	}
	if err := strictjson.RejectDuplicateObjectKeys(data); err != nil {
		return fmt.Errorf("RECEIPT_INVALID: %w", err)
	}
	if decoded.SchemaVersion != 1 || !validHash(decoded.StateVersion) || !validHash(decoded.PlanHash) {
		return fmt.Errorf("RECEIPT_INVALID: unsupported version or hash")
	}
	validated, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS:            decoded.Desired.OS,
		Application:   decoded.Desired.Application,
		AppInstalled:  decoded.Desired.AppInstalled,
		KitHome:       decoded.Desired.KitHome,
		HermesHome:    decoded.Desired.HermesHome,
		HermesVersion: decoded.Desired.HermesVersion,
		Project:       decoded.Desired.Project,
		Role:          decoded.Desired.Role,
		Toolchain:     decoded.Desired.Toolchain,
	})
	if err != nil || hashJSON(snapshotOf(validated)) != decoded.StateVersion {
		return fmt.Errorf("RECEIPT_INVALID: desired state does not match state_version")
	}
	if err := validateCheckpoints(decoded.Checkpoints); err != nil {
		return err
	}
	*r = Receipt{
		schemaVersion: decoded.SchemaVersion,
		stateVersion:  decoded.StateVersion,
		planHash:      decoded.PlanHash,
		desired:       decoded.Desired,
		checkpoints:   append([]Checkpoint(nil), decoded.Checkpoints...),
	}
	return nil
}

// MarshalJSON emits the closed deterministic receipt shape and redacts all
// registered secret values from string fields.
func (r *Receipt) MarshalJSON() ([]byte, error) {
	checkpoints := r.Checkpoints()
	for i := range checkpoints {
		checkpoints[i].Diagnostic = redact(checkpoints[i].Diagnostic, r.redactions)
	}
	return json.Marshal(receiptJSON{
		SchemaVersion: r.schemaVersion,
		StateVersion:  r.stateVersion,
		PlanHash:      r.planHash,
		Desired:       r.desired,
		Checkpoints:   checkpoints,
	})
}

// RetryActions selects incomplete idempotent effects in original plan order.
func RetryActions(plan OperationPlan, receipt *Receipt) []Action {
	actions, _ := RetryActionsChecked(plan, receipt)
	return actions
}

// RetryActionsChecked proves the receipt belongs to plan, then selects only
// its incomplete idempotent effects. A complete matching receipt returns an
// empty slice without being confused with an invalid receipt.
func RetryActionsChecked(plan OperationPlan, receipt *Receipt) ([]Action, error) {
	if receipt == nil || receipt.planHash != hashJSON(plan) || len(receipt.checkpoints) != len(plan.Actions) {
		return nil, fmt.Errorf("RECEIPT_PLAN_MISMATCH")
	}
	for i, plannedAction := range plan.Actions {
		if receipt.checkpoints[i].ActionID != plannedAction.ID {
			return nil, fmt.Errorf("RECEIPT_PLAN_MISMATCH")
		}
	}
	var retry []Action
	for i, plannedAction := range plan.Actions {
		if receipt.checkpoints[i].Status == EffectSucceeded {
			continue
		}
		if !plannedAction.Idempotent {
			return nil, fmt.Errorf("RECEIPT_NON_IDEMPOTENT_ACTION: %s", plannedAction.ID)
		}
		retry = append(retry, plannedAction)
	}
	return retry, nil
}

func (r *Receipt) firstIncomplete() int {
	for i := range r.checkpoints {
		if r.checkpoints[i].Status != EffectSucceeded {
			return i
		}
	}
	return -1
}

func hashJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("hash deterministic value: %v", err))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func nonemptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if len(result[i]) == len(result[j]) {
			return result[i] < result[j]
		}
		return len(result[i]) > len(result[j])
	})
	return result
}

func redact(value string, secrets []string) string {
	for _, secret := range secrets {
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	return value
}

func snapshotOf(desired domain.DesiredState) desiredSnapshot {
	return desiredSnapshot{
		OS:            desired.OS(),
		Application:   desired.Application(),
		AppInstalled:  desired.AppInstalled(),
		KitHome:       desired.KitHome(),
		HermesHome:    desired.HermesHome(),
		HermesVersion: desired.HermesVersion(),
		Project:       desired.Project(),
		Role:          desired.Role(),
		Toolchain:     desired.Toolchain(),
	}
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("RECEIPT_INVALID: trailing JSON value")
		}
		return fmt.Errorf("RECEIPT_INVALID: %w", err)
	}
	return nil
}

func validHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateCheckpoints(checkpoints []Checkpoint) error {
	seen := make(map[string]struct{}, len(checkpoints))
	incomplete := false
	for _, checkpoint := range checkpoints {
		if checkpoint.ActionID == "" {
			return fmt.Errorf("RECEIPT_INVALID: empty action id")
		}
		if _, duplicate := seen[checkpoint.ActionID]; duplicate {
			return fmt.Errorf("RECEIPT_INVALID: duplicate action id %q", checkpoint.ActionID)
		}
		seen[checkpoint.ActionID] = struct{}{}
		switch checkpoint.Status {
		case EffectSucceeded:
			if incomplete {
				return fmt.Errorf("RECEIPT_INVALID: succeeded action follows incomplete checkpoint")
			}
		case EffectPending, EffectFailed:
			incomplete = true
		default:
			return fmt.Errorf("RECEIPT_INVALID: unknown checkpoint status %q", checkpoint.Status)
		}
	}
	return nil
}
