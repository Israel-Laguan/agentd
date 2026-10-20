package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"agentd/internal/models"
)

// mixColumns pairs each input section with its column title.
var mixColumns = []struct{ key, title string }{
	{"system", "SYSTEM%"}, {"memories", "MEMORIES%"}, {"task", "TASK%"}, {"parent_outcome", "PARENT%"},
	{"comments", "COMMENTS%"}, {"file_context", "FILE%"}, {"history", "HISTORY%"}, {"tool_results", "TOOL%"},
}

// cutColumns pairs each cutting layer with its column title (calls cut by it).
var cutColumns = []struct{ key, title string }{
	{"working_zone", "ZONE"}, {"strategy_budget", "BUDGET"}, {"agentic_head", "HEAD"}, {"router", "ROUTER"},
}

// printAIMix writes the context-composition view: each section's share of the
// input chars, the share of calls whose input was cut, and how many were cut
// by each layer.
func printAIMix(w io.Writer, report models.AIUsageReport) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	header := make([]string, 0, len(report.GroupBy)+16)
	for _, d := range report.GroupBy {
		header = append(header, strings.ToUpper(d))
	}
	header = append(header, "CALLS", "INPUT_CHARS")
	for _, c := range mixColumns {
		header = append(header, c.title)
	}
	header = append(header, "CUT%")
	for _, c := range cutColumns {
		header = append(header, c.title)
	}
	_, _ = fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, row := range report.Rows {
		_, _ = fmt.Fprintln(tw, mixLine(report.GroupBy, row))
	}
	totals := report.Totals
	totals.Group = map[string]string{}
	if len(report.GroupBy) > 0 {
		totals.Group[report.GroupBy[0]] = "TOTAL"
	}
	_, _ = fmt.Fprintln(tw, mixLine(report.GroupBy, totals))
	return tw.Flush()
}

func mixLine(dims []string, r models.AIUsageRow) string {
	cols := make([]string, 0, len(dims)+16)
	for _, d := range dims {
		cols = append(cols, orDash(r.Group[d]))
	}
	cols = append(cols, fmt.Sprint(r.Calls), fmt.Sprint(r.InputChars))
	for _, c := range mixColumns {
		cols = append(cols, percent(r.MixChars[c.key], r.InputChars))
	}
	cols = append(cols, percent(r.TruncatedCalls, r.Calls))
	for _, c := range cutColumns {
		cols = append(cols, fmt.Sprint(r.CutBy[c.key]))
	}
	return strings.Join(cols, "\t")
}

func percent(part, whole int) string {
	if whole <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", float64(part)*100/float64(whole))
}
