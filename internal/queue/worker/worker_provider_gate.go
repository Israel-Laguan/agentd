package worker

import (
	"context"
	"errors"
	"fmt"

	"agentd/internal/models"
	"agentd/internal/queue/safety"
)

// errProviderProbeInFlight means the provider breaker is HALF_OPEN and another
// task holds its probe slot: the provider is not known to be down, so the task
// waits instead of being handed off.
var errProviderProbeInFlight = errors.New("provider probe in flight")

// admitProvider asks the provider's breaker whether this task may run. err is
// errProviderProbeInFlight while another task's probe is unresolved, and wraps
// ErrLLMQuotaExceeded when the breaker is OPEN. After the open timeout it admits
// one probe task; release gives that probe slot back when the task ends without
// recording an outcome (it never changes breaker state).
func (w *Worker) admitProvider(provider string) (release func(), err error) {
	noop := func() {}
	if w.providerBreakers == nil || provider == "" {
		return noop, nil
	}
	b := w.providerBreakers.Get(provider)
	switch b.Admit() {
	case safety.AdmissionProbe:
		return b.ReleaseProbe, nil
	case safety.AdmissionProbeInFlight:
		return noop, errProviderProbeInFlight
	case safety.AdmissionDenied:
		return noop, fmt.Errorf("%w: provider %s circuit breaker is open", models.ErrLLMQuotaExceeded, provider)
	default:
		return noop, nil
	}
}

// gateProvider runs the provider breaker gate for a task. ok is false when the
// task was requeued (probe in flight) or handed off (breaker open) and the caller
// must stop. The requeue carries no payload: the task is polled again every tick
// while the probe runs, and a RETRY event each time would flood its history.
func (w *Worker) gateProvider(ctx context.Context, task models.Task, provider string) (release func(), ok bool) {
	release, err := w.admitProvider(provider)
	switch {
	case errors.Is(err, errProviderProbeInFlight):
		w.requeue(ctx, task, "")
		return nil, false
	case err != nil:
		w.handoffOrFail(ctx, task, err)
		return nil, false
	}
	return release, true
}

// recordProviderSuccess closes the task's provider breaker after a task completed.
func (w *Worker) recordProviderSuccess(ctx context.Context, task models.Task) {
	if w.providerBreakers == nil {
		return
	}
	if provider := w.lookupProvider(ctx, task); provider != "" {
		w.providerBreakers.Get(provider).RecordSuccess()
	}
}
