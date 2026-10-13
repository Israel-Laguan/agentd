package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

type askOptions struct {
	apiURL string
	client *http.Client
}

func newAskCommand(opts *rootOptions) *cobra.Command {
	askOpts := &askOptions{client: http.DefaultClient}
	cmd := &cobra.Command{
		Use:   "ask <prompt>",
		Short: "Draft a project plan and ask for approval",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAsk(cmd, opts, askOpts, args[0])
		},
	}
	cmd.Flags().StringVar(&askOpts.apiURL, "api-url", "", "agentd API base URL")
	return cmd
}

func runAsk(cmd *cobra.Command, opts *rootOptions, askOpts *askOptions, prompt string) error {
	cfg, err := config.Load(config.LoadOptions{
		HomeOverride: opts.home,
		ConfigFile:   opts.configFile,
	})
	if err != nil {
		return err
	}
	baseURL := askOpts.baseURL(cfg)
	plan, err := draftPlan(cmd, askOpts.client, baseURL, prompt)
	if err != nil {
		return err
	}
	if err := printPlan(cmd, plan); err != nil {
		return err
	}
	approved, err := approved(cmd)
	if err != nil || !approved {
		if err == nil {
			err = writeLine(cmd.OutOrStdout(), "plan rejected")
		}
		return err
	}
	return materializePlan(cmd, askOpts.client, baseURL, cfg, plan)
}

func (o *askOptions) baseURL(cfg config.Config) string {
	if o.apiURL != "" {
		return strings.TrimRight(o.apiURL, "/")
	}
	return "http://" + strings.TrimRight(cfg.API.Address, "/")
}

func draftPlan(cmd *cobra.Command, client *http.Client, baseURL, prompt string) (models.DraftPlan, error) {
	req := map[string]any{"model": "agentd", "messages": []gateway.PromptMessage{{Role: "user", Content: prompt}}}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(req); err != nil {
		return models.DraftPlan{}, err
	}
	resp, err := client.Post(baseURL+"/v1/chat/completions", "application/json", &body)
	if err != nil {
		return models.DraftPlan{}, err
	}
	defer resp.Body.Close() //nolint:errcheck
	return decodeDraft(resp)
}

func decodeDraft(resp *http.Response) (models.DraftPlan, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return models.DraftPlan{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return models.DraftPlan{}, mapDraftAPIError(resp.StatusCode, body)
	}
	var decoded struct {
		Choices []struct {
			Message gateway.PromptMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		// A 200 with a non-JSON body is a provider-path failure (proxy error
		// page, plain-text upstream error), never a plan bug (B-044).
		return models.DraftPlan{}, nonJSONDraftError(resp.StatusCode, body)
	}
	if len(decoded.Choices) == 0 {
		return models.DraftPlan{}, fmt.Errorf("draft response missing choices")
	}
	raw := decoded.Choices[0].Message.Content
	if strings.TrimSpace(raw) == "" {
		return models.DraftPlan{}, fmt.Errorf("draft response missing plan content")
	}
	var probe struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(raw), &probe); err == nil && probe.Kind != "" {
		switch probe.Kind {
		case "feasibility_clarification", "intent_clarification", "scope_clarification", "status_report":
			return models.DraftPlan{}, nonPlanError(probe.Kind, raw)
		}
	}
	if !json.Valid([]byte(raw)) {
		// The chat endpoint answers 200 with plain-text content when every
		// provider is down (the J09 [SYSTEM] timeout message); surface the
		// provider failure instead of a JSON decode artifact (B-044).
		if isProviderOutageText(raw) {
			return models.DraftPlan{}, fmt.Errorf("draft request failed: %w: provider outage instead of plan: %s",
				config.ErrNoLLMProviders, draftBodySnippet([]byte(raw)))
		}
		return models.DraftPlan{}, fmt.Errorf("draft request failed: planner returned non-plan content instead of a draft plan: %s",
			draftBodySnippet([]byte(raw)))
	}
	var plan models.DraftPlan
	if err = json.Unmarshal([]byte(raw), &plan); err != nil {
		return models.DraftPlan{}, err
	}
	return plan, nil
}

// nonPlanError surfaces the message of a status_report or clarification reply
// so the user sees what agentd said rather than a generic failure.
func nonPlanError(kind, raw string) error {
	var reply struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(raw), &reply); err != nil || strings.TrimSpace(reply.Message) == "" {
		return fmt.Errorf("planner returned %s instead of a draft plan; refine your prompt or use the API directly", kind)
	}
	return fmt.Errorf("no plan drafted (%s): %s", kind, strings.TrimSpace(reply.Message))
}

func mapDraftAPIError(status int, body []byte) error {
	text := string(body)
	switch {
	case strings.Contains(text, "no LLM providers configured"),
		strings.Contains(text, "no LLM providers available"),
		strings.Contains(text, "LLM provider unreachable"):
		return fmt.Errorf("draft request failed (HTTP %d): %w", status, config.ErrNoLLMProviders)
	case status == http.StatusUnprocessableEntity && apiErrorMessage(body) != "":
		return fmt.Errorf("draft request failed: %s", apiErrorMessage(body))
	default:
		// Never drop the body: plain-text 5xx, 404 model-not-found text,
		// and HTML error pages carry the real diagnosis (B-044).
		if snippet := draftBodySnippet(body); snippet != "" {
			return fmt.Errorf("draft request failed (HTTP %d %s): %s", status, http.StatusText(status), snippet)
		}
		return fmt.Errorf("draft request failed (HTTP %d %s) with empty response body", status, http.StatusText(status))
	}
}

// maxDraftBodySnippet bounds how much of a non-JSON response body is echoed
// in an error so HTML error pages stay readable.
const maxDraftBodySnippet = 200

// draftBodySnippet collapses a response body to a single line for error
// messages. It returns "" for an empty body.
func draftBodySnippet(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	if text == "" {
		return ""
	}
	if runes := []rune(text); len(runes) > maxDraftBodySnippet {
		return string(runes[:maxDraftBodySnippet]) + "..."
	}
	return text
}

// isProviderOutageText matches plain-text bodies that mean the provider path
// is down rather than the plan being bad: the J09 [SYSTEM] timeout message
// and timeout/unreachable/no-provider phrases.
func isProviderOutageText(text string) bool {
	lowered := strings.ToLower(text)
	for _, signal := range []string{"[system]", "timed out", "llm provider unreachable", "no llm providers"} {
		if strings.Contains(lowered, signal) {
			return true
		}
	}
	return false
}

// nonJSONDraftError reports a 200 response whose body is not a chat
// completion at all. Only an outage-shaped body wraps config.ErrNoLLMProviders
// (so the CLI prints the provider hint); anything else is an ordinary error
// that names the body.
func nonJSONDraftError(status int, body []byte) error {
	snippet := draftBodySnippet(body)
	if snippet == "" {
		return fmt.Errorf("draft request failed (HTTP %d) with empty response body", status)
	}
	// Judge the whole body: an outage phrase can sit past the snippet limit.
	if isProviderOutageText(string(body)) {
		return fmt.Errorf("draft request failed (HTTP %d): %w: provider outage response: %s",
			status, config.ErrNoLLMProviders, snippet)
	}
	return fmt.Errorf("draft request failed (HTTP %d): unexpected non-JSON response: %s", status, snippet)
}

func apiErrorMessage(body []byte) string {
	var env struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return ""
	}
	return env.Error.Message
}

func printPlan(cmd *cobra.Command, plan models.DraftPlan) error {
	if err := writeFormat(cmd.OutOrStdout(), "Proposed project: %s\n", plan.ProjectName); err != nil {
		return err
	}
	for i, task := range plan.Tasks {
		if err := writeFormat(cmd.OutOrStdout(), "%d. %s\n   %s\n", i+1, task.Title, task.Description); err != nil {
			return err
		}
	}
	return nil
}

func approved(cmd *cobra.Command) (bool, error) {
	if err := writeFormat(cmd.OutOrStdout(), "Do you approve this plan? [Y/n] "); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "" || answer == "y" || answer == "yes", nil
}

func materializePlan(cmd *cobra.Command, client *http.Client, baseURL string, cfg config.Config, plan models.DraftPlan) error {
	if strings.TrimSpace(plan.SourcePath) == "" {
		// Like chat clients, approve an unseeded plan as an explicit empty
		// workspace; otherwise its tasks stay PENDING until /workspace/ready.
		plan.StartEmptyWorkspace = true
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(plan); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/projects/materialize", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := strings.TrimSpace(cfg.API.MaterializeToken); tok != "" {
		req.Header.Set("X-Agentd-Materialize-Token", tok)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("materialize request failed with status %s", resp.Status)
	}
	return writeLine(cmd.OutOrStdout(), "project started")
}
