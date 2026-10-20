package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/spf13/cobra"

	"agentd/internal/models"
)

func newReportCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "report", Short: "Print reports built from the daemon's records"}
	cmd.AddCommand(newReportAICommand(opts))
	return cmd
}

type reportAIFlags struct {
	apiURL, groupBy, from, to, task, request, purpose, stage, model string
	asJSON                                                          bool
}

func (f reportAIFlags) query() url.Values {
	v := url.Values{}
	for key, value := range map[string]string{
		"group_by": f.groupBy, "from": f.from, "to": f.to, "task_id": f.task,
		"request": f.request, "purpose": f.purpose, "stage": f.stage, "model": f.model,
	} {
		if value != "" {
			v.Set(key, value)
		}
	}
	return v
}

func newReportAICommand(opts *rootOptions) *cobra.Command {
	var f reportAIFlags
	cmd := &cobra.Command{
		Use:   "ai",
		Short: "Where AI calls and tokens go, by purpose, stage, model, day, task or chat request",
		RunE: func(cmd *cobra.Command, args []string) error {
			base, err := resolveAPIBase(opts, f.apiURL)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			raw, err := fetchAIUsage(cmd.Context(), base, f.query())
			if err != nil {
				return err
			}
			if f.asJSON {
				return printJSON(cmd, raw)
			}
			var report models.AIUsageReport
			if err := json.Unmarshal(raw, &report); err != nil {
				return fmt.Errorf("decode ai usage report: %w", err)
			}
			return printAIUsage(cmd.OutOrStdout(), report)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.apiURL, "api-url", "", "daemon API base URL (default: from config or http://127.0.0.1:8765)")
	fl.StringVar(&f.groupBy, "group-by", "", "comma list of purpose, stage, model, day, task, request (default purpose)")
	fl.StringVar(&f.from, "from", "", "start, YYYY-MM-DD or RFC3339 (inclusive)")
	fl.StringVar(&f.to, "to", "", "end, YYYY-MM-DD (that whole day) or RFC3339 (exclusive)")
	fl.StringVar(&f.task, "task", "", "only this task ID")
	fl.StringVar(&f.request, "request", "", "only this chat request (correlation ID)")
	fl.StringVar(&f.purpose, "purpose", "", "only this purpose, e.g. exec.turn")
	fl.StringVar(&f.stage, "stage", "", "only this stage, e.g. exec")
	fl.StringVar(&f.model, "model", "", "only this model")
	fl.BoolVar(&f.asJSON, "json", false, "print the report as JSON")
	return cmd
}

// fetchAIUsage calls GET /api/v1/reports/ai-usage and returns the data object.
func fetchAIUsage(ctx context.Context, base string, query url.Values) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/reports/ai-usage?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build report request: %w", err)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		if isConnectionError(err) {
			return nil, errors.New("daemon is not running — start with 'agentd start'")
		}
		return nil, fmt.Errorf("report request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("daemon returned HTTP %d with an unreadable body: %w", resp.StatusCode, err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("daemon returned HTTP %d %s: %s", resp.StatusCode, envelope.Error.Code, envelope.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon returned HTTP %d", resp.StatusCode)
	}
	return envelope.Data, nil
}

func printJSON(cmd *cobra.Command, raw json.RawMessage) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return fmt.Errorf("format report: %w", err)
	}
	buf.WriteByte('\n')
	_, err := cmd.OutOrStdout().Write(buf.Bytes())
	return err
}
