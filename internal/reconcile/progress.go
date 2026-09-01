package reconcile

import "context"

// ProgressPhase identifies a lifecycle boundary of one mutation effect.
type ProgressPhase string

const (
	ProgressStarted   ProgressPhase = "started"
	ProgressCompleted ProgressPhase = "completed"
	ProgressFailed    ProgressPhase = "failed"
)

// ProgressTarget identifies the typed mutation subject being reported.
type ProgressTarget uint8

const (
	ProgressAction ProgressTarget = iota + 1
	ProgressHermesCredentials
	ProgressHermesHome
)

// ProgressEvent contains only non-sensitive mutation metadata.
type ProgressEvent struct {
	Target      ProgressTarget
	Phase       ProgressPhase
	Action      ActionKind
	Application string
}

// ProgressObserver receives request-scoped mutation lifecycle events.
type ProgressObserver func(ProgressEvent)

type progressObserverKey struct{}

// WithProgressObserver attaches an observer to one operation request.
func WithProgressObserver(ctx context.Context, observer ProgressObserver) context.Context {
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, progressObserverKey{}, observer)
}

// ReportProgress delivers an event without allowing observer failures to alter mutation results.
func ReportProgress(ctx context.Context, event ProgressEvent) {
	if ctx == nil {
		return
	}
	observer, _ := ctx.Value(progressObserverKey{}).(ProgressObserver)
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer(event)
}
