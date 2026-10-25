//go:build live

package intake

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"agentd/internal/gateway/routing"
	"agentd/internal/gateway/spec"
	"agentd/internal/stage"
)

// liveFlagCase is one request with the answer the planner should give. A run
// is right when a task is flagged for a case that needs a workspace change, and
// when no task is flagged for a text-only case.
type liveFlagCase struct {
	request        string
	needsWorkspace bool
}

var liveFlagCases = []liveFlagCase{
	{"Add a --verbose flag to the CLI in main.go that prints each file name it reads.", true},
	{"Create a README.md that explains how to build and run the project.", true},
	{"Fix the failing test in parser_test.go so it passes.", true},
	{"what os and hardware have this machine", true},
	{"Write a short summary of what this project does. Reply in text only and do not change any files.", false},
	{"Explain the trade-offs of using SQLite versus Postgres for a small app like this one.", false},
}

// TestLiveRequiresWorkspaceChangeFlag measures how often the intake plan prompt
// sets requires_workspace_change correctly on a real provider (PR 11, B-085).
// It runs only on demand: go test -tags live -run TestLiveRequiresWorkspaceChangeFlag -v ./internal/stage/intake/
// It needs OPENROUTER_API_KEY. AGENTD_LIVE_MODEL overrides the model, and
// AGENTD_LIVE_RUNS sets the runs per case (default 10).
func TestLiveRequiresWorkspaceChangeFlag(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("OPENROUTER_API_KEY is not set")
	}
	model := os.Getenv("AGENTD_LIVE_MODEL")
	if model == "" {
		model = "nvidia/nemotron-3-ultra-550b-a55b:free"
	}
	runs := 10
	if v, err := strconv.Atoi(os.Getenv("AGENTD_LIVE_RUNS")); err == nil && v > 0 {
		runs = v
	}
	gw, err := routing.NewRouterFromConfigs([]spec.ProviderConfig{{
		Name:      "openrouter",
		Adapter:   "openai",
		BaseURL:   "https://openrouter.ai/api/v1",
		APIKeyEnv: "OPENROUTER_API_KEY",
		Model:     model,
		Timeout:   2 * time.Minute,
	}})
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	agent := stage.NewModelAgent(gw)

	for _, c := range liveFlagCases {
		right, errs := 0, 0
		for i := range runs {
			plan, err := Plan(context.Background(), agent, spec.PurposeIntakeDraftPlan, 0, c.request)
			if err != nil {
				errs++
				t.Logf("run %d error: %v", i+1, err)
				continue
			}
			flagged := false
			for _, task := range plan.Tasks {
				flagged = flagged || task.RequiresWorkspaceChange
			}
			if flagged == c.needsWorkspace {
				right++
			}
		}
		t.Logf("model=%s needs_workspace=%t right=%d/%d errors=%d request=%q",
			model, c.needsWorkspace, right, runs, errs, c.request)
	}
}
