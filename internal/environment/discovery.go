package environment

import (
	"context"
	"errors"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
)

const maxWarningBytes = 1536

// CandidateSource identifies where an environment candidate originated.
type CandidateSource uint8

const (
	SourceExplicit CandidateSource = iota
	SourceRegistry
	SourceEnvironment
	SourceManual
)

// Candidate identifies a root and the source that supplied it.
type Candidate struct {
	Home   string
	Source CandidateSource
}

// DiscoveryRequest contains the only supported environment candidate sources.
type DiscoveryRequest struct {
	ExplicitHome    string
	Explicit        bool
	RegistryHomes   []string
	EnvironmentHome string
}

// DiscoveryResult contains displayable environments and bounded diagnostics.
type DiscoveryResult struct {
	Environments   []VerifiedEnvironment
	Warnings       []Warning
	ManualRequired bool
}

// Warning describes a skipped candidate without retaining its underlying error.
type Warning struct {
	Source CandidateSource
	Home   string
	State  InspectionState
}

// String renders a terminal-safe, bounded diagnostic for a skipped candidate.
func (w Warning) String() string {
	message := "Предупреждение: источник=" + w.Source.String() + " путь=" + DisplayPath(w.Home) + " состояние=" + w.State.String()
	if len(message) <= maxWarningBytes {
		return message
	}
	return "Предупреждение: источник=" + w.Source.String() + " путь=\"...\" состояние=" + w.State.String()
}

// String returns a stable, non-secret source label.
func (s CandidateSource) String() string {
	switch s {
	case SourceExplicit:
		return "--kit-home"
	case SourceRegistry:
		return "реестр"
	case SourceEnvironment:
		return "KIT_ALL_TEAM_HOME"
	case SourceManual:
		return "ручной ввод"
	default:
		return "неизвестный источник"
	}
}

// Discover inspects candidates in strict source order and returns only verified roots.
func Discover(ctx context.Context, request DiscoveryRequest, inspector Inspector) (DiscoveryResult, error) {
	candidates := make([]Candidate, 0, len(request.RegistryHomes)+1)
	if request.Explicit {
		candidates = append(candidates, Candidate{Home: request.ExplicitHome, Source: SourceExplicit})
	} else {
		for _, home := range request.RegistryHomes {
			candidates = append(candidates, Candidate{Home: home, Source: SourceRegistry})
		}
		if request.EnvironmentHome != "" {
			candidates = append(candidates, Candidate{Home: request.EnvironmentHome, Source: SourceEnvironment})
		}
	}

	result := DiscoveryResult{}
	seen := make(map[string]struct{}, len(candidates))
	rawFailures := make(map[string]struct{})
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return DiscoveryResult{}, err
		}
		if pathErr := ValidateTerminalPath(candidate.Home); pathErr != nil {
			if _, duplicate := rawFailures[candidate.Home]; duplicate {
				continue
			}
			rawFailures[candidate.Home] = struct{}{}
			typed := inspectionError(Foreign, "candidate path is unsafe for terminal use", pathErr)
			if candidate.Source == SourceExplicit || candidate.Source == SourceManual {
				return DiscoveryResult{}, typed
			}
			result.Warnings = append(result.Warnings, Warning{Source: candidate.Source, Home: candidate.Home, State: Foreign})
			continue
		}
		key, keyErr := pathsafe.ComparisonKey(candidate.Home)
		if keyErr != nil {
			if _, duplicate := rawFailures[candidate.Home]; duplicate {
				continue
			}
			rawFailures[candidate.Home] = struct{}{}
			state := classifyCandidateComparisonFailure(keyErr)
			typed := inspectionError(state, "candidate path cannot be compared safely", keyErr)
			if candidate.Source == SourceExplicit || candidate.Source == SourceManual {
				return DiscoveryResult{}, typed
			}
			result.Warnings = append(result.Warnings, Warning{Source: candidate.Source, Home: candidate.Home, State: state})
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		if err := ctx.Err(); err != nil {
			return DiscoveryResult{}, err
		}
		verified, state, inspectErr := inspector.Inspect(ctx, candidate.Home)
		switch state {
		case Ready:
			if inspectErr != nil {
				return DiscoveryResult{}, inspectionError(InspectionFailed, "ready inspection returned an error", inspectErr)
			}
			if pathErr := ValidateTerminalPath(verified.Home); pathErr != nil {
				if candidate.Source == SourceExplicit || candidate.Source == SourceManual {
					return DiscoveryResult{}, inspectionError(Foreign, "verified path is unsafe for terminal use", pathErr)
				}
				result.Warnings = append(result.Warnings, Warning{Source: candidate.Source, Home: verified.Home, State: Foreign})
				continue
			}
			result.Environments = append(result.Environments, verified)
		case RetryRequired, Foreign, InspectionFailed:
			var typed *Error
			if !errors.As(inspectErr, &typed) || typed.State != state {
				return DiscoveryResult{}, inspectionError(InspectionFailed, "inspection state and typed error disagree", inspectErr)
			}
			if state == RetryRequired {
				if pathErr := ValidateTerminalPath(verified.Home); pathErr != nil {
					if candidate.Source == SourceExplicit || candidate.Source == SourceManual {
						return DiscoveryResult{}, inspectionError(Foreign, "verified path is unsafe for terminal use", pathErr)
					}
					result.Warnings = append(result.Warnings, Warning{Source: candidate.Source, Home: verified.Home, State: Foreign})
					continue
				}
				result.Environments = append(result.Environments, verified)
				continue
			}
			if candidate.Source == SourceExplicit || candidate.Source == SourceManual {
				return DiscoveryResult{}, typed
			}
			result.Warnings = append(result.Warnings, Warning{Source: candidate.Source, Home: candidate.Home, State: state})
		default:
			return DiscoveryResult{}, inspectionError(InspectionFailed, "inspector returned an unknown state", inspectErr)
		}
	}
	result.ManualRequired = len(result.Environments) == 0
	return result, nil
}

func classifyCandidateComparisonFailure(err error) InspectionState {
	if errors.Is(err, pathsafe.ErrUnsafe) {
		return Foreign
	}
	return InspectionFailed
}
