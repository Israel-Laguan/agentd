package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"agentd/internal/models"
)

func newReportToolsCommand(opts *rootOptions) *cobra.Command {
	var apiURL, from, to, task string
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "Tool calls by tool and the files a task read more than once",
		RunE: func(cmd *cobra.Command, args []string) error {
			base, err := resolveAPIBase(opts, apiURL)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			query := url.Values{}
			for k, v := range map[string]string{"from": from, "to": to, "task_id": task} {
				if v != "" {
					query.Set(k, v)
				}
			}
			raw, err := fetchReport(cmd.Context(), base, "tool-use", query)
			if err != nil {
				return err
			}
			var report models.ToolUseReport
			if err := json.Unmarshal(raw, &report); err != nil {
				return fmt.Errorf("decode tool use report: %w", err)
			}
			return printToolUse(cmd.OutOrStdout(), report)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&apiURL, "api-url", "", "daemon API base URL (default: from config or http://127.0.0.1:8765)")
	fl.StringVar(&from, "from", "", "start, YYYY-MM-DD or RFC3339 (inclusive)")
	fl.StringVar(&to, "to", "", "end, YYYY-MM-DD (that whole day) or RFC3339 (exclusive)")
	fl.StringVar(&task, "task", "", "only this task ID")
	return cmd
}

func printToolUse(w io.Writer, r models.ToolUseReport) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "TOOL\tCALLS\tFAILED\tRESULT_BYTES")
	for _, t := range r.Tools {
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%d\n", t.Tool, t.Calls, t.Failed, t.ResultBytes)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if err := writeFormat(w, "\nre-read: %d of %d bytes read (%d read calls)\n", r.RereadBytes, r.ReadBytes, r.Reads); err != nil {
		return err
	}
	if len(r.RepeatedReads) == 0 {
		return nil
	}
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "TASK\tPATH\tREADS\tBYTES\tREREAD_BYTES")
	for _, rr := range r.RepeatedReads {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\n", rr.TaskID, rr.Path, rr.Reads, rr.Bytes, rr.RereadBytes)
	}
	return tw.Flush()
}
