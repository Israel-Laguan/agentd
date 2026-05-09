package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"

	"github.com/spf13/cobra"
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
	var decoded struct {
		Choices []struct {
			Message gateway.PromptMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return models.DraftPlan{}, err
	}
	if resp.StatusCode != http.StatusOK || len(decoded.Choices) == 0 {
		return models.DraftPlan{}, fmt.Errorf("draft request failed with status %s", resp.Status)
	}
	raw := decoded.Choices[0].Message.Content
	var probe struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(raw), &probe); err == nil && probe.Kind != "" {
		switch probe.Kind {
		case "feasibility_clarification", "intent_clarification", "scope_clarification", "status_report":
			return models.DraftPlan{}, fmt.Errorf("planner returned %s instead of a draft plan; refine your prompt or use the API directly", probe.Kind)
		}
	}
	var plan models.DraftPlan
	err := json.Unmarshal([]byte(raw), &plan)
	return plan, err
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
