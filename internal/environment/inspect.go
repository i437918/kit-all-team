package environment

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/mi1man-cmd/kit-all-team/internal/config"
	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/state"
)

const (
	maxOwnerBytes     = 256
	maxPublicEnvBytes = 65536
)

var (
	errHomeMismatch           = errors.New("HOME_MISMATCH")
	errPublicMetadataTooLarge = errors.New("PUBLIC_METADATA_TOO_LARGE")
)

type inspector struct {
	comparisonKey func(string) (string, error)
}

// NewInspector constructs a read-only environment inspector.
func NewInspector() Inspector { return inspector{comparisonKey: pathsafe.ComparisonKey} }

func (i inspector) Inspect(ctx context.Context, root string) (VerifiedEnvironment, InspectionState, error) {
	_ = ctx
	if err := ValidateTerminalPath(root); err != nil {
		return foreign("root is unsafe for terminal use", err)
	}
	if root == "" || !filepath.IsAbs(root) {
		return foreign("root must be an absolute path", nil)
	}
	if err := pathsafe.ValidateDirectory(root); err != nil {
		return classifyPathFailure("root is unsafe", err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return foreign("root does not exist", err)
		}
		return inspectionFailure("cannot inspect root", err)
	}
	if !rootInfo.IsDir() {
		return foreign("root is not a directory", nil)
	}

	metadata := filepath.Join(root, ".teamkit")
	if err := pathsafe.ValidateDirectory(metadata); err != nil {
		return classifyPathFailure("metadata directory is unsafe", err)
	}
	metadataInfo, err := os.Lstat(metadata)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return foreign("metadata directory is missing", err)
		}
		return inspectionFailure("cannot inspect metadata directory", err)
	}
	if !metadataInfo.IsDir() {
		return foreign("metadata path is not a directory", nil)
	}

	operationPath := filepath.Join(metadata, "operation.json")
	if _, err := os.Lstat(operationPath); err == nil {
		verified, inspectionState, inspectionErr := inspectOperation(root, i.comparisonKey)
		if inspectionErr != nil || inspectionState == RetryRequired {
			return verified, inspectionState, inspectionErr
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return inspectionFailure("cannot inspect operation document", err)
	}

	owner, err := readPublicRegular(filepath.Join(metadata, "owner"), maxOwnerBytes)
	if err != nil {
		return classifyPublicMetadataFailure("owner metadata is invalid", err)
	}
	env, err := readPublicRegular(filepath.Join(root, ".env"), maxPublicEnvBytes)
	if err != nil {
		return classifyPublicMetadataFailure("public environment is invalid", err)
	}
	if !utf8.Valid(owner) || !utf8.Valid(env) {
		return foreign("public metadata is not UTF-8", nil)
	}
	desired, err := config.ParseDotenv(string(env))
	if err != nil {
		return foreign("public environment is malformed", err)
	}
	if err := requireSameHome(i.comparisonKey, root, desired.KitHome()); err != nil {
		return classifyComparisonFailure(err)
	}
	if strings.TrimSpace(string(owner)) != string(desired.Project()) {
		return foreign("owner does not match project", nil)
	}
	return VerifiedEnvironment{Home: desired.KitHome(), Desired: desired}, Ready, nil
}

func (i inspector) ClassifyAdd(ctx context.Context, root string) (AddState, error) {
	if err := ValidateTerminalPath(root); err != nil {
		return AddTargetReady, inspectionError(Foreign, "root is unsafe for terminal use", err)
	}
	if root == "" || !filepath.IsAbs(root) {
		return AddTargetReady, inspectionError(Foreign, "root must be an absolute path", nil)
	}
	if err := pathsafe.ValidateDirectory(root); err != nil {
		return AddTargetReady, classifyAddPathFailure("root is unsafe", err)
	}
	info, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return AddTargetReady, nil
	}
	if err != nil {
		return AddTargetReady, inspectionError(InspectionFailed, "cannot inspect add target", err)
	}
	if !info.IsDir() {
		return AddTargetReady, inspectionError(Foreign, "add target is not a directory", nil)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return AddTargetReady, inspectionError(InspectionFailed, "cannot read add target", err)
	}
	if len(entries) == 0 {
		return AddTargetReady, nil
	}
	_, inspectionState, err := i.Inspect(ctx, root)
	if inspectionState == Ready && err == nil {
		return AddWorkspaceExists, nil
	}
	if err != nil {
		return AddTargetReady, err
	}
	return AddTargetReady, inspectionError(inspectionState, "add target is not ready", nil)
}

func inspectOperation(root string, comparisonKey func(string) (string, error)) (VerifiedEnvironment, InspectionState, error) {
	store, err := state.New(root)
	if err != nil {
		return foreign("operation root is invalid", err)
	}
	plan, receipt, err := store.LoadOperation()
	if err != nil {
		return classifyOperationFailure(err)
	}
	desired, err := receipt.DesiredState()
	if err != nil {
		return foreign("operation receipt is invalid", err)
	}
	if err := requireSameHome(comparisonKey, root, desired.KitHome()); err != nil {
		return classifyComparisonFailure(err)
	}
	actions, err := reconcile.RetryActionsChecked(plan, receipt)
	if err != nil {
		return foreign("operation receipt does not match plan", err)
	}
	if len(actions) != 0 {
		verified := VerifiedEnvironment{Home: desired.KitHome(), Desired: desired, Pending: true}
		return verified, RetryRequired, inspectionError(RetryRequired, "operation receipt has incomplete actions", nil)
	}
	return VerifiedEnvironment{}, Ready, nil
}

func readPublicRegular(path string, limit int64) ([]byte, error) {
	data, err := pathsafe.ReadRegular(path, limit)
	if errors.Is(err, pathsafe.ErrTooLarge) {
		return nil, errPublicMetadataTooLarge
	}
	return data, err
}

func requireSameHome(comparisonKey func(string) (string, error), candidate, desired string) error {
	candidateKey, err := comparisonKey(candidate)
	if err != nil {
		return err
	}
	desiredKey, err := comparisonKey(desired)
	if err != nil {
		return err
	}
	if candidateKey != desiredKey {
		return errHomeMismatch
	}
	return nil
}

func classifyPathFailure(detail string, err error) (VerifiedEnvironment, InspectionState, error) {
	if errors.Is(err, pathsafe.ErrUnsafe) {
		return foreign(detail, err)
	}
	return inspectionFailure(detail, err)
}

func classifyComparisonFailure(err error) (VerifiedEnvironment, InspectionState, error) {
	if errors.Is(err, pathsafe.ErrUnsafe) || errors.Is(err, errHomeMismatch) {
		return foreign("environment home does not match candidate", err)
	}
	return inspectionFailure("cannot compare environment home", err)
}

func classifyOperationFailure(err error) (VerifiedEnvironment, InspectionState, error) {
	if errors.Is(err, pathsafe.ErrUnsafe) {
		return foreign("operation document is unsafe", err)
	}
	if isOperationalIOError(err) {
		return inspectionFailure("cannot read operation document", err)
	}
	return foreign("operation document is invalid", err)
}

func classifyPublicMetadataFailure(detail string, err error) (VerifiedEnvironment, InspectionState, error) {
	if errors.Is(err, pathsafe.ErrUnsafe) || errors.Is(err, fs.ErrNotExist) || errors.Is(err, errPublicMetadataTooLarge) {
		return foreign(detail, err)
	}
	return inspectionFailure(detail, err)
}

func classifyAddPathFailure(detail string, err error) error {
	if errors.Is(err, pathsafe.ErrUnsafe) {
		return inspectionError(Foreign, detail, err)
	}
	return inspectionError(InspectionFailed, detail, err)
}

func isOperationalIOError(err error) bool {
	var pathError *fs.PathError
	return errors.As(err, &pathError) || errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrClosed)
}

func foreign(detail string, cause error) (VerifiedEnvironment, InspectionState, error) {
	return VerifiedEnvironment{}, Foreign, inspectionError(Foreign, detail, cause)
}

func inspectionFailure(detail string, cause error) (VerifiedEnvironment, InspectionState, error) {
	return VerifiedEnvironment{}, InspectionFailed, inspectionError(InspectionFailed, detail, cause)
}
