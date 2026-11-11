package worker

import (
	"context"

	"agentd/internal/models"
)

// RecordProviderSuccess closes the task's provider breaker.
//
// Call it only immediately after the provider request itself succeeded. A local
// completion — a breakdown roll-up, a sandbox result, a review handoff — makes no
// provider call, and recording success for one would close an OPEN circuit that
// the provider never recovered from.
//
// The task is named as the verdict's owner so that closing the breaker does not
// settle a probe slot another task is holding (B-014).
//
// The provider gate that used to run before dispatch (admitProvider, the probe
// slot, the sibling wait) went with the builtin exec path in T-070: agentd makes
// no provider request of its own for a task that runs on an external agent, so
// there is nothing to admit. What still feeds this registry is the gateway's own
// quota errors, and what still reads it is the batched L1 path.
func (w *Worker) RecordProviderSuccess(ctx context.Context, task models.Task) {
	if w.providerBreakers == nil {
		return
	}
	if provider := w.lookupProvider(ctx, task); provider != "" {
		w.providerBreakers.Get(provider).RecordSuccessFor(task.ID)
	}
}
