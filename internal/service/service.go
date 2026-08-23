// Package service composes the CLI boundary from the reconciliation core and
// its concrete state, bootstrap, Git, credential, and installer adapters.
package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mi1man-cmd/kit-all-team/internal/apps"
	"github.com/mi1man-cmd/kit-all-team/internal/bootstrap"
	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/cli"
	"github.com/mi1man-cmd/kit-all-team/internal/config"
	"github.com/mi1man-cmd/kit-all-team/internal/credentials"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/engine"
	"github.com/mi1man-cmd/kit-all-team/internal/environment"
	"github.com/mi1man-cmd/kit-all-team/internal/gitx"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/operationlock"
	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/platform"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/secrets"
	"github.com/mi1man-cmd/kit-all-team/internal/state"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

const (
	// GitCAFile is the application-local environment key used by system Git.
	GitCAFile = "GIT_SSL_CAINFO"

	// WindowsInstallerSHA256 pins the release-adjacent Hermes installer.
	WindowsInstallerSHA256 = "505dfb4c2c1052b055e3fc694a76cb7ce093a64962c7713aa294f5549c6734f5"
	// WindowsInstallerSigner is the required trusted Authenticode subject.
	WindowsInstallerSigner = "Nous Research Inc."

	// POSIXInstallerURL is the immutable upstream Hermes installer script.
	POSIXInstallerURL = "https://raw.githubusercontent.com/NousResearch/hermes-agent/f80f453ae0679347e38abc917c7f94f717bf96c5/scripts/install.sh"
	// POSIXInstallerSHA256 pins the immutable POSIX installer script.
	POSIXInstallerSHA256 = "458ed1873bec1766ccd723b8a86338fbdf1caff5d43eae45065bc448cafa2dca"
	// POSIXInstallerCommit is passed to the pinned script so its source checkout
	// cannot drift independently of the verified installer revision.
	POSIXInstallerCommit = "f80f453ae0679347e38abc917c7f94f717bf96c5"
)

const maxInstallerBytes = 4 << 20
const maxOfficeCLIBytes = 48 << 20
const maxCertificateArchiveBytes = 64 << 20
const maxHermesCommandOutput = 64 << 10
const maxDesiredStateBytes = 64 << 10
const defaultHTTPTimeout = 30 * time.Second

var errKitHomeMismatch = errors.New("KIT_HOME_MISMATCH")

// ErrWindowsHermesInstallUnverified prevents unattended execution until the
// pinned EXE's exact install-directory contract has disposable-runner evidence.
var ErrWindowsHermesInstallUnverified = errors.New("HERMES_WINDOWS_INSTALL_DIR_UNVERIFIED: install Hermes manually, set HERMES_HOME to its verified installation directory, then rerun with --app-installed=true")

// ErrHomeOverlap prevents project content and private application state from
// sharing the same filesystem tree in either nesting direction.
var ErrHomeOverlap = errors.New("HOME_PATH_OVERLAP")

// DownloadPort retrieves one bounded installer payload without executing it.
type DownloadPort interface {
	Download(context.Context, string) ([]byte, error)
}

// DownloadFunc adapts a function to DownloadPort.
type DownloadFunc func(context.Context, string) ([]byte, error)

// Download calls f.
func (f DownloadFunc) Download(ctx context.Context, url string) ([]byte, error) {
	return f(ctx, url)
}

// AskPassSession owns a temporary Git helper and its environment credentials.
type AskPassSession interface {
	Credentials() gitx.Credentials
	Close() error
}

// AskPassFactory creates one temporary helper for a mutation.
type AskPassFactory func(string, gitx.Credentials) (AskPassSession, error)

// StateStoreFactory opens non-secret workspace state without performing I/O.
type StateStoreFactory func(string) (engine.Store, error)

// OperationContractResolver returns the non-secret identity of every pinned
// input that an operation created by this binary may consume.
type OperationContractResolver func(domain.DesiredState) (string, error)

// EffectInputs are the fully assembled mutation-only bootstrap dependencies.
type EffectInputs struct {
	Git                  bootstrap.GitPort
	Installer            bootstrap.InstallerPort
	InstallerPath        string
	CertificateArchive   string
	Secrets              credentials.SecretStore
	ProfileSecrets       credentials.SecretStore
	ProfileEnvironment   map[string]string
	Profile              bootstrap.ProfilePort
	OfficeCLI            bootstrap.OfficeCLIPort
	HermesEnvironment    func(string) error
	HermesExecutable     string
	RuntimeContract      hermes.RuntimeContract
	RuntimeProbe         bootstrap.RuntimeProbe
	ToolchainMaterialize bootstrap.ToolchainMaterializer
}

// EffectsFactory permits hermetic effect tests while production uses bootstrap.Effects.
type EffectsFactory func(EffectInputs) engine.Effects

// Options contains narrow injectable operating-system boundaries. Zero values
// select production implementations.
type Options struct {
	ReadFile                 func(string) ([]byte, error)
	ApplicationLookPath      platform.LookPath
	ApplicationHome          credentials.HomeResolver
	SecretStore              credentials.StoreFactory
	StateStore               StateStoreFactory
	AskPass                  AskPassFactory
	GitRunner                gitx.Runner
	Effects                  EffectsFactory
	Downloader               DownloadPort
	Process                  platform.ProcessRunner
	HermesProfile            bootstrap.ProfilePort
	ResolveHermesRuntime     func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error)
	RuntimeProbe             bootstrap.RuntimeProbe
	ToolchainMaterialize     bootstrap.ToolchainMaterializer
	ManagedInstallReady      func(string) (bool, error)
	ManagedCertificateBundle func(string, string) (string, bool, error)
	VerifyDigest             func([]byte, string) bool
	WritePrivate             func(string, []byte) error
	TempRoot                 string
	ReleaseDir               string
	OperationContract        OperationContractResolver
}

// Service is the concrete implementation of cli.Service.
type Service struct{ options Options }

var _ cli.Service = (*Service)(nil)

// New creates a concrete service. It performs no filesystem, secret, network,
// or process work until a command method is called.
func New(options Options) *Service { return &Service{options: options} }

// Plan observes only public workspace state and never opens mutation adapters.
func (s *Service) Plan(ctx context.Context, desired domain.DesiredState, update reconcile.UpdateChoice) (reconcile.OperationPlan, error) {
	_, plan, _, err := s.planBoundToRuntime(ctx, desired, update)
	return plan, err
}

func (s *Service) planBoundToRuntime(ctx context.Context, desired domain.DesiredState, update reconcile.UpdateChoice) (domain.DesiredState, reconcile.OperationPlan, hermes.RuntimeContract, error) {
	if err := s.preflightAlternativeApplication(desired); err != nil {
		return domain.DesiredState{}, reconcile.OperationPlan{}, hermes.RuntimeContract{}, err
	}
	desired, runtimeContract, err := s.bindHermesRuntime(ctx, desired)
	if err != nil {
		return domain.DesiredState{}, reconcile.OperationPlan{}, hermes.RuntimeContract{}, err
	}
	if err := validateServicePaths(desired); err != nil {
		return domain.DesiredState{}, reconcile.OperationPlan{}, hermes.RuntimeContract{}, err
	}
	if err := s.observeManagedSources(ctx, desired); err != nil {
		return domain.DesiredState{}, reconcile.OperationPlan{}, hermes.RuntimeContract{}, err
	}
	var officeCLI bootstrap.OfficeCLIPort
	if desired.Application() == domain.AppHermes {
		asset, assetErr := resolveOfficeCLIAsset(desired.OS(), runtime.GOARCH)
		if assetErr != nil {
			return domain.DesiredState{}, reconcile.OperationPlan{}, hermes.RuntimeContract{}, assetErr
		}
		officeCLI, err = s.officeCLIProvisioner(asset, desired.HermesHome())
		if err != nil {
			return domain.DesiredState{}, reconcile.OperationPlan{}, hermes.RuntimeContract{}, err
		}
	}
	plan, err := (engine.Engine{Effects: &bootstrap.Effects{HermesExecutable: runtimeContract.Info.Executable, RuntimeContract: runtimeContract, OfficeCLI: officeCLI}}).Plan(ctx, desired, update)
	if err != nil {
		return domain.DesiredState{}, reconcile.OperationPlan{}, hermes.RuntimeContract{}, err
	}
	plan.ContractHash, err = s.operationContract()(desired)
	if err != nil {
		return domain.DesiredState{}, reconcile.OperationPlan{}, hermes.RuntimeContract{}, err
	}
	return desired, plan, runtimeContract, nil
}

// preflightAlternativeApplication verifies the questionnaire claim against a
// cataloged executable without launching the selected non-Hermes client.
func (s *Service) preflightAlternativeApplication(desired domain.DesiredState) error {
	if desired.Application() == domain.AppHermes {
		return nil
	}
	if !desired.AppInstalled() {
		return apps.ErrApplicationRequired
	}
	installed, err := platform.DetectInstalled(desired.Application(), s.options.ApplicationLookPath)
	if err != nil || !installed {
		return apps.ErrApplicationRequired
	}
	return nil
}

func (s *Service) observeManagedSources(ctx context.Context, desired domain.DesiredState) error {
	project, err := catalog.LookupProject(desired.Project())
	if err != nil {
		return err
	}
	repository := gitx.NewRepository(s.gitRunner())
	branches := []struct {
		directory string
		marker    string
		remote    string
		branch    string
	}{
		{desired.KitHome(), filepath.Join(desired.KitHome(), ".teamkit", "content.ready"), project.ContentRepository, project.ContentBranch},
		{filepath.Join(desired.KitHome(), "db"), filepath.Join(desired.KitHome(), ".teamkit", "database.ready"), project.DatabaseRepository, project.DatabaseBranch},
	}
	for _, source := range branches {
		ready, err := managedMarkerExists(source.marker)
		if err != nil {
			return err
		}
		exists, err := localRepositoryExists(source.directory)
		if err != nil {
			return fmt.Errorf("%w: %v", bootstrap.ErrForeignWorkspace, err)
		}
		if !ready || !exists {
			continue
		}
		if err := repository.ObserveBranch(ctx, source.directory, source.remote, source.branch); err != nil {
			return err
		}
	}
	if desired.Application() != domain.AppHermes {
		return nil
	}
	pin, err := catalog.LookupToolchain(desired.Toolchain())
	if err != nil {
		return err
	}
	toolchain := filepath.Join(desired.HermesHome(), "profiles", hermesProfileIdentity(desired), ".teamkit", "toolchain-source")
	exists, err := localRepositoryExists(toolchain)
	if err != nil {
		return fmt.Errorf("%w: %v", bootstrap.ErrForeignProfile, err)
	}
	if exists {
		return repository.ObservePinned(ctx, toolchain, pin.Origin, pin.Commit)
	}
	return nil
}

func managedMarkerExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, bootstrap.ErrForeignWorkspace
	}
	return true, nil
}

func localRepositoryExists(directory string) (bool, error) {
	if err := pathsafe.ValidateDirectory(directory); err != nil {
		return false, err
	}
	gitDirectory := filepath.Join(directory, ".git")
	if err := pathsafe.ValidateDirectory(gitDirectory); err != nil {
		return false, err
	}
	info, err := os.Lstat(gitDirectory)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir() && info.Mode()&os.ModeSymlink == 0, nil
}

// Status strictly reloads desired state from KIT/.env and remains read-only.
func (s *Service) Status(ctx context.Context, kitHome string) (reconcile.PlanStatus, reconcile.OperationPlan, error) {
	desired, err := s.loadDesired(kitHome)
	if err != nil {
		return "", reconcile.OperationPlan{}, err
	}
	persisted, err := s.stateStore(desired.KitHome())
	if err != nil {
		return "", reconcile.OperationPlan{}, err
	}
	if atomic, ok := persisted.(interface {
		LoadOperation(...string) (reconcile.OperationPlan, *reconcile.Receipt, error)
	}); ok {
		operation, receipt, loadErr := atomic.LoadOperation()
		if loadErr == nil {
			operationDesired, recoveredVersion, hydrationErr := desiredWithReceiptHermesVersion(desired, receipt)
			if hydrationErr != nil {
				return "", reconcile.OperationPlan{}, hydrationErr
			}
			contractHash, contractErr := s.operationContract()(operationDesired)
			if contractErr != nil {
				return "", reconcile.OperationPlan{}, contractErr
			}
			if operation.ContractHash != contractHash {
				if _, legacyErr := s.validateLegacyRC2Operation(ctx, desired, operation, receipt); legacyErr != nil {
					return "", reconcile.OperationPlan{}, fmt.Errorf("OPERATION_CONTRACT_MISMATCH")
				}
			} else {
				desired = operationDesired
			}
			if !receipt.MatchesDesired(desired) {
				return "", reconcile.OperationPlan{}, fmt.Errorf("RECEIPT_DESIRED_MISMATCH")
			}
			incomplete, retryErr := reconcile.RetryActionsChecked(operation, receipt)
			if retryErr != nil {
				return "", reconcile.OperationPlan{}, retryErr
			}
			if len(incomplete) > 0 {
				if recoveredVersion {
					if _, runtimeErr := s.hermesExecutable(ctx, desired); runtimeErr != nil {
						return "", reconcile.OperationPlan{}, runtimeErr
					}
				}
				return reconcile.StatusNeedsApply, reconcile.OperationPlan{
					ContractHash: operation.ContractHash,
					Actions:      incomplete,
				}, nil
			}
		} else if !errors.Is(loadErr, os.ErrNotExist) {
			return "", reconcile.OperationPlan{}, loadErr
		}
	}
	plan, err := s.Plan(ctx, desired, reconcile.UpdateNone)
	if err != nil {
		return "", reconcile.OperationPlan{}, err
	}
	return reconcile.Status(plan), plan, nil
}

// Apply upserts supplied application-local secrets and executes a checkpointed plan.
func (s *Service) Apply(ctx context.Context, desired domain.DesiredState, update reconcile.UpdateChoice, inputs cli.ApplyInputs) (plan reconcile.OperationPlan, err error) {
	if err := s.preflightAlternativeApplication(desired); err != nil {
		return reconcile.OperationPlan{}, err
	}
	if _, err := s.validateMutationHomeOverlap(desired); err != nil {
		return reconcile.OperationPlan{}, err
	}
	if err := preflightOwnership(desired); err != nil {
		return reconcile.OperationPlan{}, err
	}
	validatedDesired, plan, _, err := s.planBoundToRuntime(ctx, desired, update)
	if err != nil {
		return reconcile.OperationPlan{}, err
	}
	if len(plan.Actions) == 0 {
		return plan, nil
	}
	mutationLock, err := acquireWorkspaceMutation(desired)
	if err != nil {
		return plan, err
	}
	defer finishOperationLock(&err, mutationLock)
	if err := s.preflightAlternativeApplication(desired); err != nil {
		return reconcile.OperationPlan{}, err
	}
	if _, err := s.validateMutationHomeOverlap(desired); err != nil {
		return reconcile.OperationPlan{}, err
	}
	if err := preflightOwnership(desired); err != nil {
		return reconcile.OperationPlan{}, err
	}
	desired, plan, runtimeContract, err := s.planBoundToRuntime(ctx, desired, update)
	if err != nil || len(plan.Actions) == 0 {
		return plan, err
	}
	if !sameDesiredState(validatedDesired, desired) {
		return plan, fmt.Errorf("HERMES_RUNTIME_DRIFT: version changed while acquiring mutation lock")
	}
	values := cloneValues(inputs.Secrets)
	persisted, err := s.stateStore(desired.KitHome())
	if err != nil {
		return plan, err
	}
	if err := (engine.Engine{Store: persisted, Secrets: valueList(values), ContractHash: plan.ContractHash}).Prepare(desired, plan); err != nil {
		return plan, err
	}
	if err := persistWorkspaceState(desired); err != nil {
		return plan, err
	}
	operation, cleanup, err := s.mutation(desired, inputs, values, true, false, runtimeContract)
	if err != nil {
		return plan, redactError(err, valueList(values))
	}
	defer finishMutation(&err, cleanup, values)
	operation.ContractHash = plan.ContractHash
	if err := operation.ExecutePrepared(ctx, desired, plan); err != nil {
		return plan, err
	}
	return plan, nil
}

// Retry strictly reloads desired state and replays only incomplete actions.
func (s *Service) Retry(ctx context.Context, kitHome string) (err error) {
	desired, err := s.loadDesired(kitHome)
	if err != nil {
		return err
	}
	initialPublicDesired := desired
	persisted, err := s.stateStore(desired.KitHome())
	if err != nil {
		return err
	}
	atomic, ok := persisted.(interface {
		LoadOperation(...string) (reconcile.OperationPlan, *reconcile.Receipt, error)
	})
	if !ok {
		return fmt.Errorf("ATOMIC_OPERATION_STORE_REQUIRED")
	}
	plan, receipt, err := atomic.LoadOperation()
	if err != nil {
		return err
	}
	operationDesired, recoveredVersion, err := desiredWithReceiptHermesVersion(desired, receipt)
	if err != nil {
		return err
	}
	contractHash, err := s.operationContract()(operationDesired)
	if err != nil {
		return err
	}
	legacyRuntimeContract := hermes.RuntimeContract{}
	legacyOperation := false
	legacyContractHash := ""
	if plan.ContractHash != contractHash {
		legacyRuntimeContract, err = s.validateLegacyRC2Operation(ctx, desired, plan, receipt)
		if err != nil {
			return fmt.Errorf("OPERATION_CONTRACT_MISMATCH")
		}
		legacyOperation = true
		legacyContractHash = plan.ContractHash
		contractHash = plan.ContractHash
	} else {
		desired = operationDesired
	}
	if !receipt.MatchesDesired(desired) {
		return fmt.Errorf("RECEIPT_DESIRED_MISMATCH")
	}
	actions, err := reconcile.RetryActionsChecked(plan, receipt)
	if err != nil {
		return err
	}
	if len(actions) == 0 {
		return nil
	}
	if recoveredVersion {
		if _, err := s.hermesExecutable(ctx, desired); err != nil {
			return err
		}
	}
	initialDesired := desired
	if err := s.preflightAlternativeApplication(desired); err != nil {
		return err
	}
	if err := preflightOwnership(desired); err != nil {
		return err
	}
	mutationLock, err := acquireWorkspaceMutation(desired)
	if err != nil {
		return err
	}
	defer finishOperationLock(&err, mutationLock)
	reloadedDesired, err := s.loadDesired(kitHome)
	if err != nil {
		return err
	}
	if legacyOperation && !sameDesiredState(initialPublicDesired, reloadedDesired) {
		return fmt.Errorf("OPERATION_CONTRACT_MISMATCH")
	}
	persisted, err = s.stateStore(reloadedDesired.KitHome())
	if err != nil {
		return err
	}
	atomic, ok = persisted.(interface {
		LoadOperation(...string) (reconcile.OperationPlan, *reconcile.Receipt, error)
	})
	if !ok {
		return fmt.Errorf("ATOMIC_OPERATION_STORE_REQUIRED")
	}
	plan, receipt, err = atomic.LoadOperation()
	if err != nil {
		return err
	}
	operationDesired = reloadedDesired
	if !legacyOperation {
		operationDesired, _, err = desiredWithReceiptHermesVersion(reloadedDesired, receipt)
		if err != nil {
			return err
		}
		if !sameDesiredState(initialDesired, operationDesired) {
			return fmt.Errorf("OPERATION_CONTRACT_MISMATCH")
		}
	}
	contractHash, err = s.operationContract()(operationDesired)
	if err != nil {
		return err
	}
	legacyRuntimeContract = hermes.RuntimeContract{}
	if legacyOperation {
		if plan.ContractHash != legacyContractHash || plan.ContractHash == contractHash {
			return fmt.Errorf("OPERATION_CONTRACT_MISMATCH")
		}
		legacyRuntimeContract, err = s.validateLegacyRC2Operation(ctx, desired, plan, receipt)
		if err != nil {
			return fmt.Errorf("OPERATION_CONTRACT_MISMATCH")
		}
		contractHash = plan.ContractHash
	} else if plan.ContractHash != contractHash {
		return fmt.Errorf("OPERATION_CONTRACT_MISMATCH")
	} else {
		desired = operationDesired
	}
	if !receipt.MatchesDesired(desired) {
		return fmt.Errorf("RECEIPT_DESIRED_MISMATCH")
	}
	actions, err = reconcile.RetryActionsChecked(plan, receipt)
	if err != nil {
		return err
	}
	if len(actions) == 0 {
		return nil
	}
	if err := s.preflightAlternativeApplication(desired); err != nil {
		return err
	}
	if err := preflightOwnership(desired); err != nil {
		return err
	}
	runtimeContract := legacyRuntimeContract
	if runtimeContract.Info.Executable == "" {
		_, runtimeContract, err = s.bindHermesRuntime(ctx, desired)
		if err != nil {
			return err
		}
	}
	if _, err := s.validateMutationHomeOverlap(desired); err != nil {
		return err
	}
	if err := persistWorkspaceState(desired); err != nil {
		return err
	}
	keys := requiredSecretKeys(desired, actions)
	values, store, err := s.loadSecrets(desired, keys)
	if err != nil {
		return err
	}
	operation, cleanup, err := s.mutationWithStoreExecutable(desired, cli.ApplyInputs{}, values, store, false, runtimeContract)
	if err != nil {
		return redactError(err, valueList(values))
	}
	defer finishMutation(&err, cleanup, values)
	operation.ContractHash = contractHash
	return operation.Retry(ctx, desired)
}

// Update safely establishes expected state from the public workspace and then
// binds the mutation to that exact state across lock acquisition.
func (s *Service) Update(ctx context.Context, kitHome string, update reconcile.UpdateChoice) (plan reconcile.OperationPlan, err error) {
	desired, err := s.loadDesired(kitHome)
	if err != nil {
		return reconcile.OperationPlan{}, err
	}
	return s.updateExpected(ctx, desired, &desired, update)
}

// UpdateVerified binds an interactive update to the exact public state that
// was inspected and displayed to the user.
func (s *Service) UpdateVerified(ctx context.Context, verified environment.VerifiedEnvironment, update reconcile.UpdateChoice) (plan reconcile.OperationPlan, err error) {
	expected := verified.Desired
	verifiedHome, homeErr := pathsafe.ComparisonKey(verified.Home)
	expectedHome, expectedErr := pathsafe.ComparisonKey(expected.KitHome())
	if homeErr != nil || expectedErr != nil || verifiedHome != expectedHome {
		return reconcile.OperationPlan{}, workspace.ErrChanged
	}
	return s.updateExpected(ctx, expected, nil, update)
}

func (s *Service) updateExpected(ctx context.Context, expected domain.DesiredState, preloaded *domain.DesiredState, update reconcile.UpdateChoice) (plan reconcile.OperationPlan, err error) {
	desired := expected
	if preloaded == nil {
		desired, err = s.loadDesiredForRevalidation(expected.KitHome())
		if err != nil {
			return reconcile.OperationPlan{}, changedWorkspaceError()
		}
	} else {
		desired = *preloaded
	}
	if !sameDesiredState(expected, desired) {
		return reconcile.OperationPlan{}, changedWorkspaceError()
	}
	if err := preflightOwnership(expected); err != nil {
		return reconcile.OperationPlan{}, err
	}
	mutationLock, err := acquireWorkspaceMutation(expected)
	if err != nil {
		return reconcile.OperationPlan{}, err
	}
	defer finishOperationLock(&err, mutationLock)
	desired, err = s.loadDesiredForRevalidation(expected.KitHome())
	if err != nil {
		return reconcile.OperationPlan{}, changedWorkspaceError()
	}
	if !sameDesiredState(expected, desired) {
		return reconcile.OperationPlan{}, changedWorkspaceError()
	}
	if err := s.preflightAlternativeApplication(desired); err != nil {
		return reconcile.OperationPlan{}, err
	}
	if _, err := s.validateMutationHomeOverlap(desired); err != nil {
		return reconcile.OperationPlan{}, err
	}
	if err := preflightOwnership(desired); err != nil {
		return reconcile.OperationPlan{}, err
	}
	desired, plan, runtimeContract, err := s.planBoundToRuntime(ctx, desired, update)
	if err != nil || len(plan.Actions) == 0 {
		return plan, err
	}
	persisted, err := s.stateStore(desired.KitHome())
	if err != nil {
		return plan, err
	}
	if err := (engine.Engine{Store: persisted, ContractHash: plan.ContractHash}).Prepare(desired, plan); err != nil {
		return plan, err
	}
	keys := requiredSecretKeys(desired, plan.Actions)
	values, store, err := s.loadSecrets(desired, keys)
	if err != nil {
		return plan, err
	}
	operation, cleanup, err := s.mutationWithStoreExecutable(desired, cli.ApplyInputs{}, values, store, false, runtimeContract)
	if err != nil {
		return plan, redactError(err, valueList(values))
	}
	defer finishMutation(&err, cleanup, values)
	operation.ContractHash = plan.ContractHash
	if err := operation.ExecutePrepared(ctx, desired, plan); err != nil {
		return plan, err
	}
	return plan, nil
}

func (s *Service) mutation(desired domain.DesiredState, inputs cli.ApplyInputs, values map[string]string, save, forceAskPass bool, runtimeContract hermes.RuntimeContract) (engine.Engine, func() error, error) {
	if _, err := s.validateMutationHomeOverlap(desired); err != nil {
		return engine.Engine{}, nil, err
	}
	store, err := s.openSecretStore(desired)
	if err != nil {
		return engine.Engine{}, nil, err
	}
	if save && len(values) > 0 {
		if _, err := store.Save(values); err != nil {
			return engine.Engine{}, nil, err
		}
	}
	return s.mutationWithStoreExecutable(desired, inputs, values, store, forceAskPass, runtimeContract)
}

func (s *Service) mutationWithStoreExecutable(desired domain.DesiredState, inputs cli.ApplyInputs, values map[string]string, store credentials.SecretStore, forceAskPass bool, runtimeContract hermes.RuntimeContract) (engine.Engine, func() error, error) {
	if _, err := s.validateMutationHomeOverlap(desired); err != nil {
		return engine.Engine{}, nil, err
	}
	applicationHome, err := s.applicationHome()(desired)
	if err != nil {
		return engine.Engine{}, nil, err
	}
	installer, installerPath, err := s.installerFor(desired, inputs, applicationHome)
	if err != nil {
		return engine.Engine{}, nil, err
	}
	certificateArchive, err := s.certificateFor(desired, inputs.CertificateArchive, applicationHome)
	if err != nil {
		return engine.Engine{}, nil, err
	}
	certificateBundle, err := s.materializeCertificateCA(desired, certificateArchive)
	if err != nil {
		return engine.Engine{}, nil, err
	}
	var officeCLI bootstrap.OfficeCLIPort
	if desired.Application() == domain.AppHermes {
		asset, assetErr := resolveOfficeCLIAsset(desired.OS(), runtime.GOARCH)
		if assetErr != nil {
			return engine.Engine{}, nil, assetErr
		}
		officeCLI, err = s.officeCLIProvisioner(asset, desired.HermesHome())
		if err != nil {
			return engine.Engine{}, nil, err
		}
	}

	var session AskPassSession
	credentialsForGit := gitx.Credentials{Username: values[credentials.GitLabUsername], Token: values[credentials.GitLabToken], CAFile: values[GitCAFile]}
	if certificateBundle != "" {
		credentialsForGit.CAFile = certificateBundle
	}
	needsGit := forceAskPass || credentialsForGit.Username != "" || credentialsForGit.Token != "" || credentialsForGit.CAFile != ""
	if needsGit {
		var err error
		session, err = s.askPassFactory()(s.tempRoot(), credentialsForGit)
		if err != nil {
			return engine.Engine{}, nil, err
		}
		credentialsForGit = session.Credentials()
	}
	cleanup := func() error {
		if session == nil {
			return nil
		}
		return session.Close()
	}
	repository := gitx.NewRepository(s.gitRunner())
	git := authenticatedGit{repository: repository, credentials: credentialsForGit}
	var profileStore credentials.SecretStore
	var profile bootstrap.ProfilePort
	profileEnvironment := map[string]string{}
	if desired.Application() == domain.AppHermes {
		profileHome := filepath.Join(desired.HermesHome(), "profiles", hermesProfileIdentity(desired))
		profileStore, err = s.secretStoreFactory()(profileHome)
		if err != nil {
			_ = cleanup()
			return engine.Engine{}, nil, err
		}
		profileEnvironment = map[string]string{
			credentials.PublicProviderAPIKey: values[credentials.PublicProviderAPIKey],
			credentials.JiraToken:        values[credentials.JiraToken],
			credentials.ConfluenceToken:  values[credentials.ConfluenceToken],
		}
		profile = s.hermesProfile(desired, runtimeContract.Info.Executable)
	}
	inputsForEffects := EffectInputs{
		Git: git, Installer: installer, InstallerPath: installerPath,
		CertificateArchive: certificateArchive, Secrets: store,
		ProfileSecrets: profileStore, ProfileEnvironment: profileEnvironment, Profile: profile,
		OfficeCLI:            officeCLI,
		HermesEnvironment:    platform.ConfigureHermesHome,
		HermesExecutable:     runtimeContract.Info.Executable,
		RuntimeContract:      runtimeContract,
		RuntimeProbe:         s.runtimeProbe(desired),
		ToolchainMaterialize: s.options.ToolchainMaterialize,
	}
	effects := s.effectsFactory()(inputsForEffects)
	mutationEffects := ownershipEffects{Effects: effects}
	persisted, err := s.stateStore(desired.KitHome())
	if err != nil {
		_ = cleanup()
		return engine.Engine{}, nil, err
	}
	return engine.Engine{Effects: mutationEffects, Store: persisted, Secrets: valueList(values)}, cleanup, nil
}

func (s *Service) loadDesired(kitHome string) (domain.DesiredState, error) {
	return s.loadDesiredWithReceipt(kitHome, true)
}

func (s *Service) loadDesiredForRevalidation(kitHome string) (domain.DesiredState, error) {
	return s.loadDesiredWithReceipt(kitHome, false)
}

func (s *Service) loadDesiredWithReceipt(kitHome string, allowReceipt bool) (domain.DesiredState, error) {
	if strings.TrimSpace(kitHome) == "" || !filepath.IsAbs(kitHome) {
		return domain.DesiredState{}, fmt.Errorf("KIT_HOME_INVALID")
	}
	clean := filepath.Clean(kitHome)
	if err := validateKitServicePaths(clean); err != nil {
		return domain.DesiredState{}, err
	}
	environmentPath := filepath.Join(clean, ".env")
	read := s.options.ReadFile
	var data []byte
	var err error
	if read != nil {
		if err := pathsafe.ValidateRegular(environmentPath); err != nil {
			return domain.DesiredState{}, fmt.Errorf("%w: %v", bootstrap.ErrForeignWorkspace, err)
		}
		data, err = read(environmentPath)
		if len(data) > maxDesiredStateBytes {
			return domain.DesiredState{}, fmt.Errorf("%w: %w", bootstrap.ErrForeignWorkspace, pathsafe.ErrTooLarge)
		}
	} else {
		data, err = pathsafe.ReadRegular(environmentPath, maxDesiredStateBytes)
	}
	var desired domain.DesiredState
	if errors.Is(err, os.ErrNotExist) {
		if !allowReceipt {
			return domain.DesiredState{}, fmt.Errorf("DESIRED_STATE_LOAD_FAILED: %w", err)
		}
		persisted, storeErr := s.stateStore(clean)
		loader, ok := persisted.(interface {
			LoadOperation(...string) (reconcile.OperationPlan, *reconcile.Receipt, error)
		})
		if storeErr != nil || !ok {
			return domain.DesiredState{}, fmt.Errorf("DESIRED_STATE_LOAD_FAILED: %w", err)
		}
		_, receipt, operationErr := loader.LoadOperation()
		if operationErr != nil {
			return domain.DesiredState{}, fmt.Errorf("DESIRED_STATE_LOAD_FAILED: %w", err)
		}
		desired, operationErr = receipt.DesiredState()
		if operationErr != nil {
			return domain.DesiredState{}, operationErr
		}
		err = nil
	} else if err != nil {
		if errors.Is(err, pathsafe.ErrUnsafe) || errors.Is(err, pathsafe.ErrTooLarge) {
			return domain.DesiredState{}, fmt.Errorf("%w: %v", bootstrap.ErrForeignWorkspace, err)
		}
		return domain.DesiredState{}, fmt.Errorf("DESIRED_STATE_LOAD_FAILED: %w", err)
	} else {
		if !utf8.Valid(data) {
			return domain.DesiredState{}, fmt.Errorf("%w: public environment is not UTF-8", bootstrap.ErrForeignWorkspace)
		}
		desired, err = config.ParseDotenv(string(data))
	}
	if err != nil {
		return domain.DesiredState{}, err
	}
	if filepath.Clean(desired.KitHome()) != clean {
		return domain.DesiredState{}, errKitHomeMismatch
	}
	return desired, nil
}

func changedWorkspaceError() error {
	return fmt.Errorf("%w: public workspace state could not be revalidated", workspace.ErrChanged)
}

func (s *Service) loadSecrets(desired domain.DesiredState, keys []string) (map[string]string, credentials.SecretStore, error) {
	store, err := s.openSecretStore(desired)
	if err != nil {
		return nil, nil, err
	}
	values := map[string]string{}
	if len(keys) > 0 {
		values, err = store.Load(keys...)
		if err != nil {
			return nil, nil, err
		}
	}
	missing := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == GitCAFile {
			continue
		}
		if strings.TrimSpace(values[key]) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, nil, fmt.Errorf("CREDENTIALS_REQUIRED: %s", strings.Join(missing, ","))
	}
	return values, store, nil
}

func (s *Service) openSecretStore(desired domain.DesiredState) (credentials.SecretStore, error) {
	home, err := s.applicationHome()(desired)
	if err != nil {
		return nil, err
	}
	if err := rejectHomeOverlap(desired.KitHome(), home); err != nil {
		return nil, err
	}
	if err := validateApplicationHome(home); err != nil {
		return nil, err
	}
	return s.secretStoreFactory()(home)
}

func (s *Service) installerFor(desired domain.DesiredState, inputs cli.ApplyInputs, applicationHome string) (bootstrap.InstallerPort, string, error) {
	if desired.Application() != domain.AppHermes || desired.AppInstalled() {
		return nil, "", nil
	}
	if err := rejectHomeOverlap(desired.KitHome(), desired.HermesHome()); err != nil {
		return nil, "", err
	}
	if err := validateHermesServicePaths(desired.HermesHome()); err != nil {
		return nil, "", err
	}
	if desired.OS() == domain.OSWindows {
		return nil, "", ErrWindowsHermesInstallUnverified
	}
	if desired.OS() != domain.OSLinux && desired.OS() != domain.OSALTLinux && desired.OS() != domain.OSMacOS {
		return nil, "", platform.ErrUnsupportedOS
	}
	installer := posixInstaller{
		downloader: s.downloader(maxInstallerBytes, "HERMES_INSTALLER_TOO_LARGE"),
		verify:     s.digestVerifier(),
		write:      s.privateWriter(),
		process:    s.processRunner(),
		ready:      s.options.ManagedInstallReady,
		installDir: filepath.Join(desired.HermesHome(), ".teamkit", "hermes-agent-source"),
		hermesHome: desired.HermesHome(),
		commit:     POSIXInstallerCommit,
	}
	if inputs.HermesInstaller != "" {
		if !filepath.IsAbs(inputs.HermesInstaller) {
			return nil, "", fmt.Errorf("HERMES_INSTALLER_PATH_INVALID")
		}
		installer.sourcePath = filepath.Clean(inputs.HermesInstaller)
	}
	if !filepath.IsAbs(applicationHome) {
		return nil, "", fmt.Errorf("APPLICATION_HOME_INVALID")
	}
	if err := rejectHomeOverlap(desired.KitHome(), applicationHome); err != nil {
		return nil, "", err
	}
	if err := validateApplicationHome(applicationHome); err != nil {
		return nil, "", err
	}
	path := filepath.Join(applicationHome, ".teamkit", "cache", "hermes-install-f80f453a.sh")
	return installer, path, nil
}

func (s *Service) certificateFor(desired domain.DesiredState, supplied, applicationHome string) (string, error) {
	if desired.Application() != domain.AppHermes {
		return "", nil
	}
	if !filepath.IsAbs(applicationHome) {
		return "", fmt.Errorf("APPLICATION_HOME_INVALID")
	}
	if err := validateApplicationHome(applicationHome); err != nil {
		return "", err
	}
	if err := rejectHomeOverlap(desired.KitHome(), applicationHome); err != nil {
		return "", err
	}
	if err := validateHermesServicePaths(desired.HermesHome()); err != nil {
		return "", err
	}
	cache := filepath.Join(applicationHome, ".teamkit", "cache", "certs.zip")
	source := supplied
	if source == "" {
		if info, err := os.Lstat(cache); err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			source = cache
		} else {
			candidate := filepath.Join(s.releaseDir(), "certs.zip")
			if candidateInfo, candidateErr := os.Lstat(candidate); candidateErr == nil && candidateInfo.Mode().IsRegular() && candidateInfo.Mode()&os.ModeSymlink == 0 {
				source = candidate
			}
		}
	}
	if source == "" {
		_, ready, err := s.managedCertificateBundle()(desired.HermesHome(), bootstrap.DefaultCertificateSHA256)
		if err != nil {
			return "", err
		}
		if ready {
			return "", nil
		}
		return "", bootstrap.ErrCertificateRequired
	}
	if !filepath.IsAbs(source) {
		return "", fmt.Errorf("CERTIFICATE_ARCHIVE_PATH_INVALID")
	}
	data, err := readBoundedFile(filepath.Clean(source), maxCertificateArchiveBytes)
	if err != nil {
		return "", err
	}
	if len(data) == 0 || !s.digestVerifier()(data, bootstrap.DefaultCertificateSHA256) {
		return "", bootstrap.ErrCertificateChecksum
	}
	if filepath.Clean(source) != filepath.Clean(cache) {
		if err := s.privateWriter()(cache, data); err != nil {
			return "", err
		}
	}
	return cache, nil
}

// materializeCertificateCA verifies the private cached archive again at the
// point of use and extracts its bundle before any Git command can run.
func (s *Service) materializeCertificateCA(desired domain.DesiredState, archive string) (string, error) {
	if desired.Application() != domain.AppHermes {
		return "", nil
	}
	if archive == "" {
		bundle, ready, err := s.managedCertificateBundle()(desired.HermesHome(), bootstrap.DefaultCertificateSHA256)
		if err != nil {
			return "", err
		}
		if !ready {
			return "", bootstrap.ErrCertificateRequired
		}
		return bundle, nil
	}
	data, err := readBoundedFile(filepath.Clean(archive), maxCertificateArchiveBytes)
	if err != nil {
		return "", err
	}
	if len(data) == 0 || !s.digestVerifier()(data, bootstrap.DefaultCertificateSHA256) {
		return "", bootstrap.ErrCertificateChecksum
	}
	return hermes.ExtractCertificates(bytes.NewReader(data), int64(len(data)), desired.HermesHome())
}

func requiredSecretKeys(desired domain.DesiredState, actions []reconcile.Action) []string {
	gitRequired, providerRequired := false, false
	for _, action := range actions {
		switch action.Kind {
		case reconcile.ActionSyncContent, reconcile.ActionSyncDatabase:
			gitRequired = true
		case reconcile.ActionConfigureApplication:
			providerRequired = desired.Application() == domain.AppHermes
		}
	}
	keys := make([]string, 0, 6)
	if gitRequired {
		keys = append(keys, credentials.GitLabUsername, credentials.GitLabToken, GitCAFile)
	}
	if providerRequired {
		keys = append(keys, credentials.PublicProviderAPIKey, credentials.JiraToken, credentials.ConfluenceToken)
	}
	return keys
}

type authenticatedGit struct {
	repository  gitx.Repository
	credentials gitx.Credentials
}

func (g authenticatedGit) CloneContent(ctx context.Context, remote, branch, destination string) error {
	return g.repository.CloneContent(ctx, remote, branch, destination, g.credentials)
}
func (g authenticatedGit) UpdateContent(ctx context.Context, content, remote, branch string) error {
	return g.repository.UpdateContent(ctx, content, remote, branch, g.credentials)
}
func (g authenticatedGit) CloneDatabase(ctx context.Context, remote, workspaceRoot string) error {
	return g.repository.CloneDatabase(ctx, remote, workspaceRoot, g.credentials)
}
func (g authenticatedGit) UpdateDatabase(ctx context.Context, database, remote string) error {
	return g.repository.UpdateDatabase(ctx, database, remote, g.credentials)
}
func (g authenticatedGit) SyncPinned(ctx context.Context, remote, commit, destination string) error {
	// Toolchain origins are public GitHub repositories. Never disclose GitLab
	// credentials cross-host; only the application-local CA is propagated.
	return g.repository.SyncPinned(ctx, remote, commit, destination, gitx.Credentials{CAFile: g.credentials.CAFile})
}

type ownershipEffects struct{ engine.Effects }

func (e ownershipEffects) Observe(ctx context.Context, desired domain.DesiredState, update reconcile.UpdateChoice) (reconcile.ObservedState, error) {
	observed, err := e.Effects.Observe(ctx, desired, update)
	if err != nil {
		return reconcile.ObservedState{}, err
	}
	if err := prepareOwnership(desired); err != nil {
		return reconcile.ObservedState{}, err
	}
	return observed, nil
}

func prepareOwnership(desired domain.DesiredState) error {
	if err := preflightOwnership(desired); err != nil {
		return err
	}
	classification, err := workspace.Classify(desired.KitHome())
	if err != nil {
		return err
	}
	if classification == workspace.Empty {
		return workspace.EnsureOwner(desired.KitHome(), string(desired.Project()))
	}
	pending, err := pendingOperationMatches(desired)
	if err != nil {
		return err
	}
	if pending {
		return workspace.EnsureOwner(desired.KitHome(), string(desired.Project()))
	}
	return nil
}

func acquireWorkspaceMutation(desired domain.DesiredState) (*operationlock.Lock, error) {
	if err := prepareOwnership(desired); err != nil {
		return nil, err
	}
	lock, err := operationlock.Acquire(desired.KitHome())
	if err != nil {
		return nil, err
	}
	if err := preflightOwnership(desired); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return lock, nil
}

func finishOperationLock(target *error, lock *operationlock.Lock) {
	if closeErr := lock.Close(); closeErr != nil && *target == nil {
		*target = closeErr
	}
}

func persistWorkspaceState(desired domain.DesiredState) error {
	if err := prepareOwnership(desired); err != nil {
		return err
	}
	return workspace.WritePublicEnv(filepath.Join(desired.KitHome(), ".env"), config.Encode(desired))
}

func validateServicePaths(desired domain.DesiredState) error {
	if err := validateKitServicePaths(desired.KitHome()); err != nil {
		return err
	}
	if desired.Application() == domain.AppHermes {
		if err := rejectHomeOverlap(desired.KitHome(), desired.HermesHome()); err != nil {
			return err
		}
		return validateHermesServicePaths(desired.HermesHome())
	}
	return nil
}

func (s *Service) validateMutationHomeOverlap(desired domain.DesiredState) (string, error) {
	if err := validateServicePaths(desired); err != nil {
		return "", err
	}
	applicationHome, err := s.applicationHome()(desired)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(applicationHome) {
		return "", fmt.Errorf("APPLICATION_HOME_INVALID")
	}
	if err := validateApplicationHome(applicationHome); err != nil {
		return "", err
	}
	if err := rejectHomeOverlap(desired.KitHome(), applicationHome); err != nil {
		return "", err
	}
	return filepath.Clean(applicationHome), nil
}

func rejectHomeOverlap(kitHome, privateHome string) error {
	overlap, err := pathsafe.Overlaps(kitHome, privateHome)
	if err != nil {
		return err
	}
	if overlap {
		return fmt.Errorf("%w: KIT_ALL_TEAM_HOME and application home must be separate trees", ErrHomeOverlap)
	}
	return nil
}

func validateKitServicePaths(root string) error {
	for _, directory := range []string{
		root,
		filepath.Join(root, ".teamkit"),
		filepath.Join(root, ".git"),
		filepath.Join(root, "db"),
		filepath.Join(root, "db", ".git"),
	} {
		if err := pathsafe.ValidateDirectory(directory); err != nil {
			return fmt.Errorf("%w: %v", bootstrap.ErrForeignWorkspace, err)
		}
	}
	for _, file := range []string{
		filepath.Join(root, ".env"),
		filepath.Join(root, ".gitignore"),
		filepath.Join(root, ".teamkit", "owner"),
		filepath.Join(root, ".teamkit", "content.ready"),
		filepath.Join(root, ".teamkit", "database.ready"),
		filepath.Join(root, ".teamkit", "handoff.txt"),
		filepath.Join(root, ".teamkit", "plan.json"),
		filepath.Join(root, ".teamkit", "receipt.json"),
		filepath.Join(root, ".teamkit", "operation.json"),
	} {
		if err := pathsafe.ValidateRegular(file); err != nil {
			return fmt.Errorf("%w: %v", bootstrap.ErrForeignWorkspace, err)
		}
	}
	return nil
}

func validateHermesServicePaths(home string) error {
	officeCLIAsset, err := catalog.LookupOfficeCLIAsset(domain.OSLinux, "amd64")
	if err != nil {
		return err
	}
	for _, directory := range []string{
		home,
		filepath.Join(home, "certs"),
		filepath.Join(home, "profiles"),
		filepath.Join(home, ".teamkit"),
		filepath.Join(home, ".teamkit", "profiles"),
		filepath.Join(home, ".teamkit", "cache"),
		filepath.Join(home, ".teamkit", "hermes-agent-source"),
		filepath.Join(home, ".teamkit", "officecli"),
		filepath.Join(home, ".teamkit", "officecli", officeCLIAsset.Version),
	} {
		if err := pathsafe.ValidateDirectory(directory); err != nil {
			return fmt.Errorf("%w: %v", bootstrap.ErrForeignProfile, err)
		}
	}
	if err := pathsafe.ValidateRegular(filepath.Join(home, "certs", "ca-bundle.pem")); err != nil {
		return fmt.Errorf("%w: %v", bootstrap.ErrForeignProfile, err)
	}
	if err := pathsafe.ValidateRegular(filepath.Join(home, ".teamkit", "officecli", officeCLIAsset.Version, officeCLIExecutableName())); err != nil {
		return fmt.Errorf("%w: %v", bootstrap.ErrForeignProfile, err)
	}
	return nil
}

func validateApplicationHome(home string) error {
	for _, directory := range []string{
		home,
		filepath.Join(home, ".teamkit"),
		filepath.Join(home, ".teamkit", "cache"),
	} {
		if err := pathsafe.ValidateDirectory(directory); err != nil {
			return fmt.Errorf("%w: %v", bootstrap.ErrForeignProfile, err)
		}
	}
	for _, file := range []string{
		filepath.Join(home, ".env"),
		filepath.Join(home, ".teamkit", "cache", "certs.zip"),
		filepath.Join(home, ".teamkit", "cache", "hermes-install-f80f453a.sh"),
	} {
		if err := pathsafe.ValidateRegular(file); err != nil {
			return fmt.Errorf("%w: %v", bootstrap.ErrForeignProfile, err)
		}
	}
	return nil
}

func preflightOwnership(desired domain.DesiredState) error {
	if err := validateServicePaths(desired); err != nil {
		return err
	}
	root := desired.KitHome()
	info, err := os.Lstat(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return bootstrap.ErrForeignWorkspace
	}
	classification, err := workspace.Classify(root)
	if err != nil {
		return err
	}
	if classification == workspace.Empty {
		return nil
	}
	metadataDir := filepath.Join(root, ".teamkit")
	metadataInfo, err := os.Lstat(metadataDir)
	if err != nil || !metadataInfo.IsDir() || metadataInfo.Mode()&os.ModeSymlink != 0 {
		return bootstrap.ErrForeignWorkspace
	}
	owner := filepath.Join(metadataDir, "owner")
	ownerInfo, err := os.Lstat(owner)
	if errors.Is(err, os.ErrNotExist) {
		pending, pendingErr := pendingOperationMatches(desired)
		if pendingErr != nil {
			return pendingErr
		}
		if pending {
			return nil
		}
		return bootstrap.ErrForeignWorkspace
	}
	if err != nil || !ownerInfo.Mode().IsRegular() || ownerInfo.Mode()&os.ModeSymlink != 0 {
		return bootstrap.ErrForeignWorkspace
	}
	data, err := os.ReadFile(owner)
	if err != nil || strings.TrimSpace(string(data)) != string(desired.Project()) {
		return bootstrap.ErrForeignWorkspace
	}
	return nil
}

// pendingOperationMatches recognizes the one safe first-run residue that may
// exist before ownership and the public .env are published. No other file or
// directory shape is adopted.
func pendingOperationMatches(desired domain.DesiredState) (bool, error) {
	root := desired.KitHome()
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(entries) != 1 || entries[0].Name() != ".teamkit" || !entries[0].IsDir() {
		return false, nil
	}
	metadata := filepath.Join(root, ".teamkit")
	if err := pathsafe.ValidateDirectory(metadata); err != nil {
		return false, err
	}
	metadataEntries, err := os.ReadDir(metadata)
	if err != nil {
		return false, err
	}
	if len(metadataEntries) != 1 || metadataEntries[0].Name() != "operation.json" || !metadataEntries[0].Type().IsRegular() {
		return false, nil
	}
	persisted, err := state.New(root)
	if err != nil {
		return false, err
	}
	_, receipt, err := persisted.LoadOperation()
	if err != nil {
		return false, nil
	}
	return receipt.MatchesDesired(desired), nil
}

type posixInstaller struct {
	downloader DownloadPort
	verify     func([]byte, string) bool
	write      func(string, []byte) error
	process    platform.ProcessRunner
	ready      func(string) (bool, error)
	sourcePath string
	installDir string
	hermesHome string
	commit     string
}

func (p posixInstaller) Apply(ctx context.Context, destination string) error {
	if !filepath.IsAbs(destination) || !filepath.IsAbs(p.installDir) || !filepath.IsAbs(p.hermesHome) || p.commit != POSIXInstallerCommit {
		return fmt.Errorf("HERMES_INSTALLER_PATH_INVALID")
	}
	var data []byte
	var err error
	if p.sourcePath != "" {
		data, err = os.ReadFile(p.sourcePath)
	} else {
		data, err = p.downloader.Download(ctx, POSIXInstallerURL)
	}
	if err != nil {
		return fmt.Errorf("HERMES_INSTALLER_DOWNLOAD_FAILED: %w", err)
	}
	if len(data) == 0 || len(data) > maxInstallerBytes || !p.verify(data, POSIXInstallerSHA256) {
		return hermes.ErrInstallerChecksum
	}
	path := destination
	if err := p.write(path, data); err != nil {
		return err
	}
	arguments := []string{
		path,
		"--dir", p.installDir,
		"--hermes-home", p.hermesHome,
		"--commit", p.commit, "--force-commit",
		"--skip-setup", "--non-interactive",
	}
	if err := (platform.FixedInstallerRunner{Executable: "/bin/bash", Arguments: arguments, RunProcess: p.process}).Run(ctx); err != nil {
		return err
	}
	return p.verifyInstalledCheckout()
}

func (p posixInstaller) verifyInstalledCheckout() error {
	check := p.ready
	if check == nil {
		check = hermes.ManagedInstallReady
	}
	ready, err := check(p.hermesHome)
	if err != nil {
		return fmt.Errorf("HERMES_INSTALL_VERIFICATION_FAILED: %w", err)
	}
	if !ready {
		return fmt.Errorf("HERMES_INSTALL_VERIFICATION_FAILED")
	}
	return nil
}

func readBoundedFile(path string, limit int64) ([]byte, error) {
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
		return nil, fmt.Errorf("HERMES_INSTALLER_TOO_LARGE")
	}
	return data, nil
}

type systemProcessRunner struct{}

func (systemProcessRunner) Run(ctx context.Context, name string, args []string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

type hermesEnvironmentRunner struct {
	home    string
	process platform.ProcessRunner
}

func (r hermesEnvironmentRunner) Run(ctx context.Context, name string, args []string) error {
	if _, production := r.process.(systemProcessRunner); production {
		command := exec.CommandContext(ctx, name, args...)
		command.Env = append(os.Environ(), "HERMES_HOME="+r.home)
		return command.Run()
	}
	return r.process.Run(ctx, name, args)
}

func (r hermesEnvironmentRunner) Capture(ctx context.Context, name string, args []string) ([]byte, error) {
	if _, production := r.process.(systemProcessRunner); !production {
		return nil, fmt.Errorf("HERMES_OUTPUT_CAPTURE_REQUIRED")
	}
	command := exec.CommandContext(ctx, name, args...)
	command.Env = append(os.Environ(), "HERMES_HOME="+r.home)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	if output.Len() > maxHermesCommandOutput {
		return nil, fmt.Errorf("HERMES_OUTPUT_TOO_LARGE")
	}
	return output.Bytes(), err
}

type httpDownloader struct {
	client        *http.Client
	maxBytes      int64
	tooLargeError string
}

func (d httpDownloader) Download(ctx context.Context, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := d.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP_STATUS_%d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, d.maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > d.maxBytes {
		return nil, fmt.Errorf("%s", d.tooLargeError)
	}
	return data, nil
}

func (s *Service) applicationHome() credentials.HomeResolver {
	if s.options.ApplicationHome != nil {
		return s.options.ApplicationHome
	}
	return credentials.DefaultApplicationHome
}

func (s *Service) hermesProfile(desired domain.DesiredState, executable string) bootstrap.ProfilePort {
	if s.options.HermesProfile != nil {
		return s.options.HermesProfile
	}
	if !desired.AppInstalled() && desired.OS() != domain.OSWindows {
		executable = filepath.Join(desired.HermesHome(), ".teamkit", "hermes-agent-source", "venv", "bin", "hermes")
	}
	return hermes.ProfileCLI{
		Executable: executable,
		Runner: hermesEnvironmentRunner{
			home: desired.HermesHome(), process: s.processRunner(),
		},
	}
}

func (s *Service) hermesExecutable(ctx context.Context, desired domain.DesiredState) (string, error) {
	_, contract, err := s.bindHermesRuntime(ctx, desired)
	return contract.Info.Executable, err
}

func (s *Service) bindHermesRuntime(ctx context.Context, desired domain.DesiredState) (domain.DesiredState, hermes.RuntimeContract, error) {
	if desired.Application() != domain.AppHermes || !desired.AppInstalled() {
		return desired, hermes.RuntimeContract{}, nil
	}
	observed, err := s.resolveHermesRuntime(ctx, desired)
	if err != nil {
		return domain.DesiredState{}, hermes.RuntimeContract{}, err
	}
	executable, err := validateVerifiedHermesRuntime(desired, observed, desired.HermesVersion())
	if err != nil {
		return domain.DesiredState{}, hermes.RuntimeContract{}, err
	}
	contract := observed.Contract
	if contract.Info.Executable == "" && s.options.ResolveHermesRuntime != nil {
		contract = hermes.RuntimeContract{
			Info:         hermes.RuntimeInfo{Executable: executable, InstallDir: filepath.Dir(executable), Version: observed.Version},
			Identity:     hermes.RuntimeIdentity{InstallRootKey: "injected-runtime-root", ExecutableKey: "injected-runtime-executable"},
			ConfigSchema: hermes.HermesConfigVersion, BundledInventorySHA256: strings.Repeat("0", 64),
		}
	}
	if contract.Info.Executable != executable || contract.Info.Version != observed.Version || contract.ConfigSchema != 34 && contract.ConfigSchema != 37 {
		return domain.DesiredState{}, hermes.RuntimeContract{}, fmt.Errorf("HERMES_RUNTIME_DRIFT: runtime contract is missing or inconsistent")
	}
	if desired.HermesVersion() != "" {
		return desired, contract, nil
	}
	if strings.TrimSpace(observed.Version) == "" {
		return domain.DesiredState{}, hermes.RuntimeContract{}, fmt.Errorf("HERMES_RUNTIME_DRIFT: observed version is missing")
	}
	bound, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS:            desired.OS(),
		Application:   desired.Application(),
		AppInstalled:  desired.AppInstalled(),
		KitHome:       desired.KitHome(),
		HermesHome:    desired.HermesHome(),
		HermesVersion: observed.Version,
		Project:       desired.Project(),
		Role:          desired.Role(),
		Toolchain:     desired.Toolchain(),
	})
	if err != nil {
		return domain.DesiredState{}, hermes.RuntimeContract{}, fmt.Errorf("HERMES_RUNTIME_DRIFT: observed version is invalid")
	}
	return bound, contract, nil
}

func (s *Service) resolveHermesRuntime(ctx context.Context, state domain.DesiredState) (hermes.DiscoveryResult, error) {
	if s.options.ResolveHermesRuntime != nil {
		return s.options.ResolveHermesRuntime(ctx, state)
	}
	installed := true
	return hermes.Discover(ctx, hermes.DiscoveryRequest{
		OS: state.OS(), ExplicitHome: state.HermesHome(), InstalledOverride: &installed, KitHome: state.KitHome(),
	}, hermes.DiscoveryDependencies{})
}

func (s *Service) validateLegacyRC2Operation(_ context.Context, desired domain.DesiredState, plan reconcile.OperationPlan, receipt *reconcile.Receipt) (hermes.RuntimeContract, error) {
	if s.options.OperationContract != nil || !receipt.MatchesDesired(desired) || !legacyRC2FailedToolchainShape(plan, receipt) {
		return hermes.RuntimeContract{}, fmt.Errorf("OPERATION_CONTRACT_MISMATCH")
	}
	legacyHash, err := legacyRC2InstalledHermesContract(desired)
	if err != nil || legacyHash == "" || plan.ContractHash != legacyHash {
		return hermes.RuntimeContract{}, fmt.Errorf("OPERATION_CONTRACT_MISMATCH")
	}

	// The exact shape is intentionally still recognized so the historical RC2
	// fixture remains immutable. It cannot be resumed: its identity predates
	// the mandatory pinned OfficeCLI asset in the current operation contract.
	return hermes.RuntimeContract{}, fmt.Errorf("OPERATION_CONTRACT_MISMATCH")
}

func validateLegacyRC2OwnershipAnchors(desired domain.DesiredState) error {
	if err := preflightOwnership(desired); err != nil {
		return err
	}
	kitOwner := filepath.Join(desired.KitHome(), ".teamkit", "owner")
	if err := validateExactLegacyMarker(kitOwner, []byte(string(desired.Project())+"\n"), bootstrap.ErrForeignWorkspace); err != nil {
		return err
	}
	identity := hermesProfileIdentity(desired)
	profile := filepath.Join(desired.HermesHome(), "profiles", identity)
	for _, directory := range []string{profile, filepath.Join(profile, ".teamkit", "toolchain-source")} {
		if err := pathsafe.ValidateDirectory(directory); err != nil {
			return err
		}
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return bootstrap.ErrForeignProfile
		}
	}
	owner := filepath.Join(desired.HermesHome(), ".teamkit", "profiles", identity+".owner")
	return validateExactLegacyMarker(owner, []byte(identity+"\n"), bootstrap.ErrForeignProfile)
}

func validateExactLegacyMarker(path string, expected []byte, foreign error) error {
	return validateExactLegacyMarkerWithReader(path, expected, foreign, pathsafe.ReadRegular)
}

func validateExactLegacyMarkerWithReader(path string, expected []byte, foreign error, read func(string, int64) ([]byte, error)) error {
	data, err := read(path, int64(len(expected)))
	if err != nil || !bytes.Equal(data, expected) {
		return foreign
	}
	return nil
}

func legacyRC2FailedToolchainShape(plan reconcile.OperationPlan, receipt *reconcile.Receipt) bool {
	expectedActions := [...]reconcile.Action{
		{ID: "10-prepare-workspace", Kind: reconcile.ActionPrepareWorkspace, Idempotent: true},
		{ID: "20-sync-content", Kind: reconcile.ActionSyncContent, Idempotent: true},
		{ID: "30-sync-database", Kind: reconcile.ActionSyncDatabase, Idempotent: true},
		{ID: "40-install-toolchain", Kind: reconcile.ActionInstallToolchain, Idempotent: true},
		{ID: "50-configure-application", Kind: reconcile.ActionConfigureApplication, Idempotent: true},
		{ID: "90-verify-state", Kind: reconcile.ActionVerifyState, Idempotent: true},
	}
	expectedStatuses := [...]reconcile.EffectStatus{
		reconcile.EffectSucceeded, reconcile.EffectSucceeded, reconcile.EffectSucceeded,
		reconcile.EffectFailed, reconcile.EffectPending, reconcile.EffectPending,
	}
	checkpoints := receipt.Checkpoints()
	if len(plan.Actions) != len(expectedActions) || len(checkpoints) != len(expectedActions) {
		return false
	}
	for index := range expectedActions {
		if plan.Actions[index] != expectedActions[index] || checkpoints[index].ActionID != expectedActions[index].ID || checkpoints[index].Status != expectedStatuses[index] {
			return false
		}
		if index == 3 {
			if checkpoints[index].Diagnostic != "toolchain skill layout is invalid" {
				return false
			}
		} else if checkpoints[index].Diagnostic != "" {
			return false
		}
	}
	return true
}

func sameDesiredState(left, right domain.DesiredState) bool {
	return left.OS() == right.OS() &&
		left.Application() == right.Application() &&
		left.AppInstalled() == right.AppInstalled() &&
		left.KitHome() == right.KitHome() &&
		left.HermesHome() == right.HermesHome() &&
		left.HermesVersion() == right.HermesVersion() &&
		left.Project() == right.Project() &&
		left.Role() == right.Role() &&
		left.Toolchain() == right.Toolchain()
}

func desiredWithReceiptHermesVersion(desired domain.DesiredState, receipt *reconcile.Receipt) (domain.DesiredState, bool, error) {
	if receipt == nil || desired.Application() != domain.AppHermes || !desired.AppInstalled() || desired.HermesVersion() != "" {
		return desired, false, nil
	}
	receiptDesired, err := receipt.DesiredState()
	if err != nil {
		return domain.DesiredState{}, false, err
	}
	if receiptDesired.HermesVersion() == "" || !sameDesiredStateExceptHermesVersion(desired, receiptDesired) {
		return desired, false, nil
	}
	return receiptDesired, true, nil
}

func sameDesiredStateExceptHermesVersion(left, right domain.DesiredState) bool {
	return left.OS() == right.OS() &&
		left.Application() == right.Application() &&
		left.AppInstalled() == right.AppInstalled() &&
		left.KitHome() == right.KitHome() &&
		left.HermesHome() == right.HermesHome() &&
		left.Project() == right.Project() &&
		left.Role() == right.Role() &&
		left.Toolchain() == right.Toolchain()
}

func validateVerifiedHermesRuntime(desired domain.DesiredState, observed hermes.DiscoveryResult, requiredVersion string) (string, error) {
	if !observed.Installed || strings.TrimSpace(observed.Executable) == "" {
		return "", fmt.Errorf("HERMES_EXECUTABLE_UNVERIFIED: runtime is not installed")
	}
	want, err := filepath.Abs(desired.HermesHome())
	if err != nil {
		return "", fmt.Errorf("HERMES_RUNTIME_DRIFT: invalid desired home")
	}
	got, err := filepath.Abs(observed.Home)
	if err != nil {
		return "", fmt.Errorf("HERMES_RUNTIME_DRIFT: invalid observed home")
	}
	equal := filepath.Clean(want) == filepath.Clean(got)
	if desired.OS() == domain.OSWindows {
		equal = strings.EqualFold(filepath.Clean(want), filepath.Clean(got))
	}
	if !equal {
		return "", fmt.Errorf("HERMES_RUNTIME_DRIFT: HERMES_HOME changed")
	}
	if requiredVersion != "" && observed.Version != requiredVersion {
		return "", fmt.Errorf("HERMES_RUNTIME_DRIFT: version changed")
	}
	return observed.Executable, nil
}

func (s *Service) managedCertificateBundle() func(string, string) (string, bool, error) {
	if s.options.ManagedCertificateBundle != nil {
		return s.options.ManagedCertificateBundle
	}
	return hermes.ManagedCertificateBundle
}

func hermesProfileIdentity(desired domain.DesiredState) string {
	return "1c-" + string(desired.Project()) + "-" + string(desired.Role()) + "-" + string(desired.Toolchain())
}
func (s *Service) secretStoreFactory() credentials.StoreFactory {
	if s.options.SecretStore != nil {
		return s.options.SecretStore
	}
	return func(home string) (credentials.SecretStore, error) { return secrets.NewStore(home) }
}
func (s *Service) stateStore(root string) (engine.Store, error) {
	if s.options.StateStore != nil {
		return s.options.StateStore(root)
	}
	return state.New(root)
}
func (s *Service) askPassFactory() AskPassFactory {
	if s.options.AskPass != nil {
		return s.options.AskPass
	}
	return func(root string, values gitx.Credentials) (AskPassSession, error) {
		return gitx.NewAskPassSession(root, values)
	}
}
func (s *Service) gitRunner() gitx.Runner {
	if s.options.GitRunner != nil {
		return s.options.GitRunner
	}
	return gitx.SystemRunner{}
}
func (s *Service) effectsFactory() EffectsFactory {
	if s.options.Effects != nil {
		return s.options.Effects
	}
	return func(input EffectInputs) engine.Effects {
		return &bootstrap.Effects{
			Git: input.Git, Installer: input.Installer, InstallerPath: input.InstallerPath,
			CertificateArchive: input.CertificateArchive, Secrets: input.Secrets,
			ProfileSecrets: input.ProfileSecrets, ProfileEnvironment: input.ProfileEnvironment, Profile: input.Profile,
			OfficeCLI:            input.OfficeCLI,
			HermesEnvironment:    input.HermesEnvironment,
			HermesExecutable:     input.HermesExecutable,
			RuntimeContract:      input.RuntimeContract,
			RuntimeProbe:         input.RuntimeProbe,
			ToolchainMaterialize: input.ToolchainMaterialize,
		}
	}
}

func (s *Service) runtimeProbe(desired domain.DesiredState) bootstrap.RuntimeProbe {
	if s.options.RuntimeProbe != nil {
		return s.options.RuntimeProbe
	}
	runner := hermesEnvironmentRunner{home: desired.HermesHome(), process: s.processRunner()}
	return func(ctx context.Context, executable string) (hermes.RuntimeContract, error) {
		return hermes.VerifyRuntimeContract(ctx, executable, runner.Capture)
	}
}
func (s *Service) downloader(maxBytes int64, tooLargeError string) DownloadPort {
	if s.options.Downloader != nil {
		return s.options.Downloader
	}
	return httpDownloader{client: &http.Client{Timeout: defaultHTTPTimeout}, maxBytes: maxBytes, tooLargeError: tooLargeError}
}
func (s *Service) processRunner() platform.ProcessRunner {
	if s.options.Process != nil {
		return s.options.Process
	}
	return systemProcessRunner{}
}
func (s *Service) digestVerifier() func([]byte, string) bool {
	if s.options.VerifyDigest != nil {
		return s.options.VerifyDigest
	}
	return func(data []byte, expected string) bool {
		digest := sha256.Sum256(data)
		return strings.EqualFold(hex.EncodeToString(digest[:]), expected)
	}
}
func (s *Service) privateWriter() func(string, []byte) error {
	if s.options.WritePrivate != nil {
		return s.options.WritePrivate
	}
	return func(path string, data []byte) error { return workspace.WriteFileAtomic(path, data, 0o600) }
}
func (s *Service) officeCLIWriter() func(string, []byte) error {
	return func(path string, data []byte) error { return workspace.WriteFileAtomic(path, data, 0o700) }
}
func (s *Service) tempRoot() string {
	if s.options.TempRoot != "" {
		return filepath.Clean(s.options.TempRoot)
	}
	return filepath.Clean(os.TempDir())
}
func (s *Service) releaseDir() string {
	if s.options.ReleaseDir != "" {
		return filepath.Clean(s.options.ReleaseDir)
	}
	executable, err := os.Executable()
	if err != nil || !filepath.IsAbs(executable) {
		return filepath.Clean(os.TempDir())
	}
	return filepath.Dir(executable)
}

func finishMutation(target *error, cleanup func() error, values map[string]string) {
	if cleanupErr := cleanup(); cleanupErr != nil && *target == nil {
		*target = cleanupErr
	}
	*target = redactError(*target, valueList(values))
}

type safeError struct {
	cause   error
	message string
}

func (e safeError) Error() string { return e.message }
func (e safeError) Unwrap() error { return e.cause }

func redactError(err error, values []string) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	for _, value := range values {
		message = strings.ReplaceAll(message, value, "[REDACTED]")
	}
	return safeError{cause: err, message: message}
}

func valueList(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return len(result[i]) > len(result[j]) })
	return result
}

func cloneValues(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
