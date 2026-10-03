//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"
)

// J16: a sibling waits for an in-flight *provider* breaker's probe instead of
// being handed to a human, and the provider's recovery closes the breaker on its
// own. This is the end-to-end proof of B-013's contract, and of B-016's fix: a
// per-provider breaker can only be fed by ErrLLMQuotaExceeded, and until B-016
// the cascade flattened that sentinel away, so no provider breaker could ever
// open and this journey was impossible to write.
//
// It runs against the provider profile (agentd-prb on :8771), whose single
// gateway provider is the mock LLM model `prb-quota`
// (devenv/agentd/config.provider.yaml). The journey owns that model name: the
// mock's POST /quota and POST /slow_once are keyed by model.
//
// Why an arming phase, and why the tasks that trip the breaker are not the three
// this journey waits on:
//
// The quota branch of HandleGatewayError records the verdict AND hands the task
// off, so whichever tasks trip a provider breaker end BLOCKED with a HUMAN child.
// There is no configuration that trips it without that, and no way for a task
// that was handed off to later reach COMPLETED. So the trip is done by three
// throwaway "arming" tasks, and the three tasks this journey actually asserts on
// are created afterwards — after breaker.open_timeout has elapsed, so the first
// of them is admitted as the probe rather than handed off as an OPEN breaker.
//
// The recovery assertion deliberately follows no reset: after the trip the journey
// never calls POST /api/v1/system/breaker/reset again, so a CLOSED breaker can
// only come from a successful probe.

const (
	// j16Provider / j16Model mirror config.provider.yaml's provider name and model.
	j16Provider = "quota"
	j16Model    = "prb-quota"

	// j16OpenTimeout mirrors breaker.open_timeout in config.provider.yaml.
	j16OpenTimeout = 10 * time.Second

	// j16TripFailures is safety.defaultBreakerFailures: the third quota failure
	// trips the provider breaker.
	j16TripFailures = 3

	// j16ProbeHold is how long the admitted probe's provider call takes. It has to
	// outlast j16ObserveWindow so the siblings' behaviour is observed while the
	// probe is genuinely still in flight, and it must be long enough that the
	// probe has not resolved when the first sibling reaches the gate.
	j16ProbeHold = 15 * time.Second

	// j16ObserveWindow is how long the journey keeps asserting "no handoff, no
	// HUMAN subtask" once the breaker is observed HALF_OPEN.
	j16ObserveWindow = 6 * time.Second

	// j16TripBound bounds the arming phase. Generous: it includes the dispatch
	// tick, three gateway round trips and the handoff writes.
	j16TripBound = 90 * time.Second

	// j16RecoveryBound bounds everything after the trip: the rest of the open
	// timeout, the slow probe, and the siblings' own runs. Deliberately built from
	// j16ProbeHold so a timeout that stopped being read from config fails here.
	j16RecoveryBound = j16OpenTimeout + j16ProbeHold + 90*time.Second
)

// TestJ16_SiblingsWaitForProviderProbe is J16.
func TestJ16_SiblingsWaitForProviderProbe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	harness := NewHarness(providerBaseURL, "provider")
	if err := harness.WaitForHealthy(ctx, 15*time.Second); err != nil {
		t.Fatalf("J16 [boot] provider profile not healthy on %s: %v", providerBaseURL, err)
	}
	client := NewAPIClient(providerBaseURL, harness.client)
	mock := NewMockLLMClient(mockLLMBaseURL)

	// Pre-arm hygiene, same as J09: the breakers are process-global and a prior
	// run may have left this provider's OPEN. This is the only reset the journey
	// makes — the recovery below has to be unaided.
	j16ResetBreaker(ctx, t, client)
	j16SetAgentProvider(ctx, t, client)
	j16SetQuota(ctx, t, mock, true)

	// Arm: three throwaway tasks against the 429 model trip the provider breaker.
	// This is the assertion that B-016 is fixed end to end — before it, these
	// failures took the global breaker's requeue path and provider_breakers stayed
	// null.
	armed := materializePlan(ctx, t, client, "J16", DraftPlan{
		ProjectName:         UniqueProjectName("j16arm"),
		Description:         "J16 arming: the provider answers 429 until its breaker trips",
		StartEmptyWorkspace: true,
		Tasks:               j16Tasks("j16arm", j16TripFailures),
	})
	j16AwaitProviderBreaker(ctx, t, client, BreakerOpen, "tripped by quota failures")
	j16AssertArmingHandoff(ctx, t, client, armed.Project.ID)
	t.Logf("J16-A: provider breaker OPEN after %d quota failures in project %s", j16TripFailures, armed.Project.ID)

	// The provider recovers, and the open timeout has to elapse before anything is
	// admitted again, or the three tasks below would be handed off as an OPEN
	// breaker instead of being waited for.
	j16SetQuota(ctx, t, mock, false)
	time.Sleep(j16OpenTimeout + 2*time.Second)

	// The probe is held in flight by the mock's one-shot delay, armed before the
	// tasks exist so the very first request after the timeout is the slow one.
	j16SetSlowOnce(ctx, t, mock, j16ProbeHold)
	project := materializePlan(ctx, t, client, "J16", DraftPlan{
		ProjectName:         UniqueProjectName("j16"),
		Description:         "J16: siblings wait for the provider breaker's probe",
		StartEmptyWorkspace: true,
		Tasks:               j16Tasks("j16", 3),
	})
	projectID := project.Project.ID

	j16AwaitProviderBreaker(ctx, t, client, BreakerHalfOpen, "probe admitted after the open timeout")
	j16AssertNoHandoffWhileProbeInFlight(ctx, t, client, projectID)
	t.Logf("J16-B: breaker HALF_OPEN with the probe in flight; no handoff and no HUMAN subtask in project %s", projectID)

	tasks, err := NewTaskPoller(client, projectID).WaitForAllComplete(ctx, j16RecoveryBound)
	if err != nil {
		t.Fatalf("J16 [complete] %v (tasks: %s)", err, j16TaskStates(tasks))
	}
	if len(tasks) != 3 {
		t.Fatalf("J16 [complete] %d tasks completed, want 3 (%s)", len(tasks), j16TaskStates(tasks))
	}
	j16AwaitProviderBreaker(ctx, t, client, BreakerClosed, "closed by the successful probe")
	t.Logf("J16-C: 3 tasks COMPLETED and the provider breaker CLOSED with no reset after the trip")
}
