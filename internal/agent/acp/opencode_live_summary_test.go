//go:build live

package acp

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func logRun(t *testing.T, rec *liveRun) {
	t.Helper()
	t.Logf("%s/%s run %d attempt %d: model=%s permit=%s reqs=%d permitter=%d refused=%d stop=%s err=%v wall=%s",
		rec.Cell, rec.Task, rec.Run, rec.Attempt, rec.ActualModel, rec.Permit,
		rec.PermissionCount, rec.PermitterCount, rec.DriverRefused, rec.StopReason, rec.Error,
		time.Duration(rec.WallMS)*time.Millisecond)
	for _, p := range rec.Permissions {
		t.Logf("  permission %d id=%s kind=%s title=%q rawInput=%s locations=%s options=%v outcome=%s option=%s permitter=%t refused=%t",
			p.Index+1, p.RequestID, p.Kind, p.Title, jsonInline(p.RawInput), jsonInline(p.Locations),
			p.Options, p.Outcome, p.SelectedOption, p.PermitterCalled, p.DriverRefused)
	}
	for _, c := range rec.PermitterCalls {
		t.Logf("  hook tool=%s kind=%s command=%q args=%s allow=%t", c.Tool, c.Kind, c.Command, truncate(c.Args, 500), c.Allow)
	}
}

// liveBatchExport is the final summary document. It is separate from liveBatch
// because the live batch also carries the mutex and its initial, empty state.
type liveBatchExport struct {
	Root            string                    `json:"root"`
	Started         string                    `json:"started"`
	OpenCodeVersion string                    `json:"opencodeVersion,omitempty"`
	Model           string                    `json:"model"`
	Runs            int                       `json:"runs"`
	Cells           []string                  `json:"cells"`
	Tasks           []string                  `json:"tasks"`
	Config          string                    `json:"config"`
	Preflights      map[string]*livePreflight `json:"preflights"`
	Records         []*liveRun                `json:"records"`
}

func writeBatchSummary(t *testing.T, batch *liveBatch) {
	t.Helper()
	preflights, records := batch.snapshot()
	sort.Slice(records, func(i, j int) bool {
		if records[i].Cell != records[j].Cell {
			return records[i].Cell < records[j].Cell
		}
		if records[i].Task != records[j].Task {
			return records[i].Task < records[j].Task
		}
		if records[i].Run != records[j].Run {
			return records[i].Run < records[j].Run
		}
		return records[i].Attempt < records[j].Attempt
	})

	meta := liveBatchExport{
		Root:            batch.Root,
		Started:         batch.Started,
		OpenCodeVersion: batch.OpenCodeVersion,
		Model:           batch.Model,
		Runs:            batch.Runs,
		Cells:           batch.Cells,
		Tasks:           batch.Tasks,
		Config:          batch.Config,
		Preflights:      preflights,
		Records:         records,
	}
	if err := writeJSON(filepath.Join(batch.Root, "summary.json"), meta); err != nil {
		t.Errorf("write summary.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(batch.Root, "summary.md"), []byte(renderMarkdown(meta)), 0o600); err != nil {
		t.Errorf("write summary.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(batch.Root, "raw-permissions.md"), []byte(renderRawPermissions(records)), 0o600); err != nil {
		t.Errorf("write raw-permissions.md: %v", err)
	}
	t.Logf("wrote %s/summary.md, summary.json and raw-permissions.md", batch.Root)
}

func renderMarkdown(meta liveBatchExport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# opencode ACP live probe\n\n")
	fmt.Fprintf(&b, "- started: %s\n", meta.Started)
	fmt.Fprintf(&b, "- opencode: %s\n", meta.OpenCodeVersion)
	fmt.Fprintf(&b, "- configured model: `%s`\n", meta.Model)
	fmt.Fprintf(&b, "- cells: %v, tasks: %v, runs per cell/task: %d\n", meta.Cells, meta.Tasks, meta.Runs)
	fmt.Fprintf(&b, "- root: `%s`\n\n", meta.Root)

	fmt.Fprintf(&b, "## Preflight (session/new)\n\n")
	fmt.Fprintf(&b, "| Cell | Configured | Actual | Free | Session | Error |\n")
	fmt.Fprintf(&b, "|---|---|---|---|---|---|\n")
	for _, cell := range []string{"A", "B", "C", "D", "E"} {
		pre := meta.Preflights[cell]
		if pre == nil {
			continue
		}
		fmt.Fprintf(&b, "| %s | `%s` | `%s` | %t | `%s` | %s |\n",
			cell, pre.ConfigModel, pre.ActualModel, pre.Free, pre.SessionID, mdCell(pre.Error))
	}
	fmt.Fprintf(&b, "\n## Runs\n\n")
	fmt.Fprintf(&b, "| Cell | Task | Run | Att | Actual model | Permit | Reqs | Hook | Refused | Stop | Error | Wall |\n")
	fmt.Fprintf(&b, "|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, rec := range meta.Records {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | `%s` | %s | %d | %d | %d | %s | %s | %s |\n",
			rec.Cell, rec.Task, rec.Run, rec.Attempt, rec.ActualModel, rec.Permit,
			rec.PermissionCount, rec.PermitterCount, rec.DriverRefused, rec.StopReason,
			mdCell(rec.Error), time.Duration(rec.WallMS)*time.Millisecond)
	}
	fmt.Fprintf(&b, "\n## Permission requests\n\n")
	fmt.Fprintf(&b, "| Cell | Task | Run | # | Kind | Title | Raw input | Locations | Options | Outcome | Hook | Driver refused |\n")
	fmt.Fprintf(&b, "|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, rec := range meta.Records {
		for _, p := range rec.Permissions {
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %s | %s | `%s` | `%s` | %s | %s | %t | %t |\n",
				rec.Cell, rec.Task, rec.Run, p.Index+1, p.Kind, mdCell(p.Title),
				mdCell(jsonInline(p.RawInput)), mdCell(jsonInline(p.Locations)), optionNames(p.Options),
				p.Outcome, p.PermitterCalled, p.DriverRefused)
		}
	}
	fmt.Fprintf(&b, "\n## Task checks\n\n")
	fmt.Fprintf(&b, "| Cell | Task | Run | Result |\n")
	fmt.Fprintf(&b, "|---|---|---|---|\n")
	for _, rec := range meta.Records {
		fmt.Fprintf(&b, "| %s | %s | %d | %s |\n", rec.Cell, rec.Task, rec.Run, mdCell(checkSummary(rec)))
	}
	return b.String()
}

func renderRawPermissions(records []*liveRun) string {
	var b strings.Builder
	b.WriteString("# Raw permission requests\n\n")
	for _, rec := range records {
		if len(rec.Permissions) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s/%s run %d attempt %d\n\n", rec.Cell, rec.Task, rec.Run, rec.Attempt)
		for _, p := range rec.Permissions {
			fmt.Fprintf(&b, "### request %d (id %s, kind %s, hook=%t, driver-refused=%t)\n\n",
				p.Index+1, p.RequestID, p.Kind, p.PermitterCalled, p.DriverRefused)
			fmt.Fprintf(&b, "```json\n%s\n```\n\n", p.RawRequest)
			if len(p.Response) > 0 {
				fmt.Fprintf(&b, "```json\n%s\n```\n\n", p.Response)
			}
		}
	}
	return b.String()
}

func checkSummary(rec *liveRun) string {
	switch rec.Task {
	case "T1":
		return fmt.Sprintf("out.txt=%v correct=%v files=%v", rec.Files["out.txt"], rec.Files["out.txt.correct"], rec.WorkspaceFiles)
	case "T2":
		return fmt.Sprintf("ran.marker=%v sawGoVersion=%t files=%v", rec.Files["ran.marker"], rec.SawGoVersion, rec.WorkspaceFiles)
	case "T3":
		return fmt.Sprintf("sawCowsay=%t files=%v text=%q", rec.SawCowsay, rec.WorkspaceFiles, truncate(rec.Text, 240))
	default:
		return fmt.Sprintf("files=%v text=%q", rec.WorkspaceFiles, truncate(rec.Text, 240))
	}
}

func optionNames(options []liveOption) string {
	var parts []string
	for _, o := range options {
		parts = append(parts, fmt.Sprintf("%s/%s/%s", o.OptionID, o.Kind, o.Name))
	}
	return mdCell(strings.Join(parts, "; "))
}
