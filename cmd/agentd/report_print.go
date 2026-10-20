package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"agentd/internal/models"
)

// printAIUsage writes one line per group plus a TOTAL line. Latency is the
// average and maximum; repair% is json_repair attempts over first attempts and
// cascade% is attempts made after a provider failed over all attempts.
func printAIUsage(w io.Writer, report models.AIUsageReport) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	header := make([]string, 0, len(report.GroupBy)+8)
	for _, d := range report.GroupBy {
		header = append(header, strings.ToUpper(d))
	}
	header = append(header, "CALLS", "TOKENS", "CACHED", "AVG_MS", "MAX_MS", "REPAIR%", "CASCADE%", "FAILED")
	_, _ = fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, row := range report.Rows {
		_, _ = fmt.Fprintln(tw, usageLine(report.GroupBy, row))
	}
	totals := report.Totals
	totals.Group = map[string]string{}
	if len(report.GroupBy) > 0 {
		totals.Group[report.GroupBy[0]] = "TOTAL"
	}
	total := usageLine(report.GroupBy, totals)
	_, _ = fmt.Fprintln(tw, total)
	if err := tw.Flush(); err != nil {
		return err
	}
	return writeFormat(w, "dropped records: %d (since the daemon started)\n", report.Dropped)
}

func usageLine(dims []string, r models.AIUsageRow) string {
	cols := make([]string, 0, len(dims)+8)
	for _, d := range dims {
		cols = append(cols, orDash(r.Group[d]))
	}
	cols = append(cols,
		fmt.Sprint(r.Calls), fmt.Sprint(r.Tokens), fmt.Sprint(r.CachedTokens),
		fmt.Sprintf("%.0f", r.AvgLatencyMS), fmt.Sprint(r.MaxLatencyMS),
		fmt.Sprintf("%.1f", r.RepairRate*100), fmt.Sprintf("%.1f", r.CascadeRate*100), fmt.Sprint(r.Failed))
	return strings.Join(cols, "\t")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
