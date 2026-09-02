// Package state persists non-secret reconciliation state beneath a workspace.
package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/strictjson"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

// Store owns the plan and receipt files for one absolute workspace root.
type Store struct {
	directory string
}

const operationSchemaVersion = 1

// MaxOperationBytes bounds every persisted operation document read by Store.
const MaxOperationBytes = 1 << 20

var errDocumentTooLarge = errors.New("DOCUMENT_TOO_LARGE")

type operationEnvelope struct {
	SchemaVersion int                     `json:"schema_version"`
	Plan          reconcile.OperationPlan `json:"plan"`
	Receipt       json.RawMessage         `json:"receipt"`
}

// New creates a state store definition without touching the filesystem.
func New(workspaceRoot string) (*Store, error) {
	if !filepath.IsAbs(workspaceRoot) {
		return nil, fmt.Errorf("STATE_ROOT_INVALID: workspace root must be absolute")
	}
	return &Store{directory: filepath.Join(filepath.Clean(workspaceRoot), ".teamkit")}, nil
}

// SavePlan atomically persists a deterministic, non-secret operation plan.
func (s *Store) SavePlan(plan reconcile.OperationPlan) error {
	data, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	return workspace.WriteFileAtomic(filepath.Join(s.directory, "plan.json"), data, 0o600)
}

// LoadPlan strictly loads and validates the persisted plan.
func (s *Store) LoadPlan() (reconcile.OperationPlan, error) {
	if present, err := s.operationPresent(); err != nil {
		return reconcile.OperationPlan{}, err
	} else if present {
		plan, _, err := s.LoadOperation()
		return plan, err
	}
	path := filepath.Join(s.directory, "plan.json")
	data, err := readBoundedRegular(path, MaxOperationBytes)
	if err != nil {
		if errors.Is(err, errDocumentTooLarge) {
			return reconcile.OperationPlan{}, fmt.Errorf("PLAN_TOO_LARGE: %w", err)
		}
		return reconcile.OperationPlan{}, err
	}
	var plan reconcile.OperationPlan
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return reconcile.OperationPlan{}, fmt.Errorf("PLAN_INVALID: %w", err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return reconcile.OperationPlan{}, err
	}
	if err := strictjson.RejectDuplicateObjectKeys(data); err != nil {
		return reconcile.OperationPlan{}, fmt.Errorf("PLAN_INVALID: %w", err)
	}
	if err := validatePlan(plan); err != nil {
		return reconcile.OperationPlan{}, err
	}
	return plan, nil
}

// SaveReceipt atomically persists a receipt after built-in redaction.
func (s *Store) SaveReceipt(receipt *reconcile.Receipt) error {
	if receipt == nil {
		return fmt.Errorf("RECEIPT_REQUIRED")
	}
	data, err := receipt.MarshalJSON()
	if err != nil {
		return err
	}
	return workspace.WriteFileAtomic(filepath.Join(s.directory, "receipt.json"), data, 0o600)
}

// SaveOperation atomically publishes the exact plan and its checkpoint receipt
// as a single generation, so interruption cannot leave a split pair.
func (s *Store) SaveOperation(plan reconcile.OperationPlan, receipt *reconcile.Receipt) error {
	if receipt == nil {
		return fmt.Errorf("RECEIPT_REQUIRED")
	}
	if err := validatePlan(plan); err != nil {
		return err
	}
	receiptData, err := receipt.MarshalJSON()
	if err != nil {
		return err
	}
	data, err := json.Marshal(operationEnvelope{
		SchemaVersion: operationSchemaVersion,
		Plan:          plan,
		Receipt:       receiptData,
	})
	if err != nil {
		return err
	}
	return workspace.WriteFileAtomic(filepath.Join(s.directory, "operation.json"), data, 0o600)
}

// LoadOperation strictly loads one atomic operation generation.
func (s *Store) LoadOperation(secrets ...string) (reconcile.OperationPlan, *reconcile.Receipt, error) {
	path := filepath.Join(s.directory, "operation.json")
	data, err := readBoundedRegular(path, MaxOperationBytes)
	if err != nil {
		if errors.Is(err, errDocumentTooLarge) {
			return reconcile.OperationPlan{}, nil, fmt.Errorf("OPERATION_TOO_LARGE: %w", err)
		}
		return reconcile.OperationPlan{}, nil, err
	}
	var envelope operationEnvelope
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return reconcile.OperationPlan{}, nil, fmt.Errorf("OPERATION_INVALID: %w", err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return reconcile.OperationPlan{}, nil, fmt.Errorf("OPERATION_INVALID: trailing JSON")
	}
	if err := strictjson.RejectDuplicateObjectKeys(data); err != nil {
		return reconcile.OperationPlan{}, nil, fmt.Errorf("OPERATION_INVALID: %w", err)
	}
	if envelope.SchemaVersion != operationSchemaVersion {
		return reconcile.OperationPlan{}, nil, fmt.Errorf("OPERATION_INVALID: unsupported version")
	}
	if err := validatePlan(envelope.Plan); err != nil {
		return reconcile.OperationPlan{}, nil, err
	}
	receipt, err := reconcile.ParseReceipt(envelope.Receipt, secrets...)
	if err != nil {
		return reconcile.OperationPlan{}, nil, err
	}
	if _, err := reconcile.RetryActionsChecked(envelope.Plan, receipt); err != nil {
		return reconcile.OperationPlan{}, nil, err
	}
	return envelope.Plan, receipt, nil
}

// LoadReceipt strictly loads a receipt and registers current secret redactions.
func (s *Store) LoadReceipt(secrets ...string) (*reconcile.Receipt, error) {
	if present, err := s.operationPresent(); err != nil {
		return nil, err
	} else if present {
		_, receipt, err := s.LoadOperation(secrets...)
		return receipt, err
	}
	path := filepath.Join(s.directory, "receipt.json")
	data, err := readBoundedRegular(path, MaxOperationBytes)
	if err != nil {
		if errors.Is(err, errDocumentTooLarge) {
			return nil, fmt.Errorf("RECEIPT_TOO_LARGE: %w", err)
		}
		return nil, err
	}
	return reconcile.ParseReceipt(data, secrets...)
}

func readBoundedRegular(path string, limit int64) ([]byte, error) {
	if err := pathsafe.ValidateRegular(path); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errDocumentTooLarge
	}
	return data, nil
}

func (s *Store) operationPresent() (bool, error) {
	path := filepath.Join(s.directory, "operation.json")
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := pathsafe.ValidateRegular(path); err != nil {
		return false, err
	}
	return true, nil
}

func validatePlan(plan reconcile.OperationPlan) error {
	seen := make(map[string]struct{}, len(plan.Actions))
	for _, action := range plan.Actions {
		if action.ID == "" || !action.Idempotent || !knownKind(action.Kind) {
			return fmt.Errorf("PLAN_INVALID: invalid action")
		}
		if _, ok := seen[action.ID]; ok {
			return fmt.Errorf("PLAN_INVALID: duplicate action %q", action.ID)
		}
		seen[action.ID] = struct{}{}
	}
	return nil
}

func knownKind(kind reconcile.ActionKind) bool {
	switch kind {
	case reconcile.ActionPrepareWorkspace, reconcile.ActionSyncContent,
		reconcile.ActionSyncDatabase, reconcile.ActionInstallToolchain,
		reconcile.ActionConfigureApplication, reconcile.ActionVerifyState:
		return true
	default:
		return false
	}
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("PLAN_INVALID: trailing JSON")
	}
	return nil
}
