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
		{"Write a short summary of what this project does. Reply in text only. Do not create, modify, or delete any files.", false},

	{"Explain the trade-offs of using SQLite versus Postgres for a small app like this one.", false},
}

// TestLiveRequiresWorkspaceChangeFlag measures how often the intake plan prompt
// sets requires_workspace_change correctly on a real provider (PR 11, B-085).
// It runs only on demand: go test -tags live -run TestLiveRequiresWorkspaceChangeFlag -v ./internal/stage/intake/
// It needs the key in the env var named by AGENTD_LIVE_API_KEY_ENV (default
// OPENROUTER_API_KEY). AGENTD_LIVE_BASE_URL and AGENTD_LIVE_MODEL override the
// endpoint and model, and AGENTD_LIVE_RUNS sets the runs per case (default 10).
func TestLiveRequiresWorkspaceChangeFlag(t *testing.T) {
	keyEnv := os.Getenv("AGENTD_LIVE_API_KEY_ENV")
	if keyEnv == "" {
		keyEnv = "OPENROUTER_API_KEY"
	}
	key := os.Getenv(keyEnv)
	if key == "" {
		t.Skip(keyEnv + " is not set")
	}
	model := os.Getenv("AGENTD_LIVE_MODEL")
	if model == "" {
		model = "nvidia/nemotron-3-ultra-550b-a55b:free"
	}
	runs := 10
	if v, err := strconv.Atoi(os.Getenv("AGENTD_LIVE_RUNS")); err == nil && v > 0 {
		runs = v
	}
	delaySec := 2
	if v, err := strconv.Atoi(os.Getenv("AGENTD_LIVE_DELAY_SEC")); err == nil && v >= 0 {
		delaySec = v
	}
	baseURL := os.Getenv("AGENTD_LIVE_BASE_URL")
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	gw, err := routing.NewRouterFromConfigs([]spec.ProviderConfig{{
		Name:      "live",
		Adapter:   "openai",
		BaseURL:   baseURL,
		APIKeyEnv: keyEnv,
		Model:     model,
		Timeout:   5 * time.Minute,
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
				continue
			}
			for _, task := range plan.Tasks {
				t.Logf("run %d wrong: task %q requires_workspace_change=%t", i+1, task.Title, task.RequiresWorkspaceChange)
			}
			time.Sleep(time.Duration(delaySec) * time.Second)
		}
		t.Logf("model=%s needs_workspace=%t right=%d/%d errors=%d request=%q",
			model, c.needsWorkspace, right, runs, errs, c.request)
		if errs > 0 {
			t.Errorf("%d of %d runs errored; the measurement is not valid", errs, runs)
		}
	}
}

// liveVagueCases are requests the plan step must answer with a question, and
// requests it must still plan (B-085). The last two are the regression guards.
var liveVagueCases = []struct {
	request string
	asks    bool
}{
	{"make my app better", true},
	{"improve it", true},
	{"what os and hardware have this machine", false},
	{"Add a --verbose flag to the CLI in main.go that prints each file name it reads.", false},
}

// TestLiveVagueRequestAsksAQuestion measures how often the plan prompt returns
// clarification_question for a vague request and tasks otherwise (PR 11, B-085).
// Run on demand like TestLiveRequiresWorkspaceChangeFlag; same environment.
func TestLiveVagueRequestAsksAQuestion(t *testing.T) {
	keyEnv := os.Getenv("AGENTD_LIVE_API_KEY_ENV")
	if keyEnv == "" {
		keyEnv = "OPENROUTER_API_KEY"
	}
	key := os.Getenv(keyEnv)
	if key == "" {
		t.Skip(keyEnv + " is not set")
	}
	model := os.Getenv("AGENTD_LIVE_MODEL")
	if model == "" {
		model = "nvidia/nemotron-3-ultra-550b-a55b:free"
	}
	runs := 10
	if v, err := strconv.Atoi(os.Getenv("AGENTD_LIVE_RUNS")); err == nil && v > 0 {
		runs = v
	}
	delaySec := 2
	if v, err := strconv.Atoi(os.Getenv("AGENTD_LIVE_DELAY_SEC")); err == nil && v >= 0 {
		delaySec = v
	}
	baseURL := os.Getenv("AGENTD_LIVE_BASE_URL")
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	gw, err := routing.NewRouterFromConfigs([]spec.ProviderConfig{{
		Name: "live", Adapter: "openai", BaseURL: baseURL,
		APIKeyEnv: keyEnv, Model: model, Timeout: 5 * time.Minute,
	}})
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	agent := stage.NewModelAgent(gw)
	for _, c := range liveVagueCases {
		right, errs := 0, 0
		for i := range runs {
			plan, err := Plan(context.Background(), agent, spec.PurposeIntakeDraftPlan, 0, c.request)
			if err != nil {
				errs++
				t.Logf("run %d error: %v", i+1, err)
				continue
			}
			if (plan.ClarificationQuestion != "") == c.asks {
				right++
			}
			time.Sleep(time.Duration(delaySec) * time.Second)
		}
		t.Logf("model=%s asks=%t right=%d/%d errors=%d request=%q", model, c.asks, right, runs, errs, c.request)
		if errs > 0 {
			t.Errorf("%d of %d runs errored; the measurement is not valid", errs, runs)
		}
	}
}
