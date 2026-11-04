//go:build live

package acp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type liveOptions struct {
	model         string
	runs          int
	cells         []string
	tasks         []string
	quotaRetries  int
	preflightOnly bool
	root          string
	bash          string
	stdbuf        string
	opencode      string
}

// liveCell is one configuration and permission policy. Tasks are the prompts
// run under it.
type liveCell struct {
	ID     string
	Config func(model string) map[string]any
	Permit string
	Tasks  []liveTask
}

// liveTask is one prompt and its workspace setup and post-run checks.
type liveTask struct {
	Name   string
	Prompt string
	Setup  func(workspace string) error
	Check  func(workspace string) map[string]any
}

func probeT1() liveTask {
	return liveTask{
		Name:   "T1",
		Prompt: "Read hello.txt and write the word it contains to out.txt.",
		Setup: func(workspace string) error {
			return os.WriteFile(filepath.Join(workspace, "hello.txt"), []byte("garnet\n"), 0o644)
		},
		Check: func(workspace string) map[string]any {
			out := map[string]any{}
			raw, err := os.ReadFile(filepath.Join(workspace, "out.txt"))
			if err != nil {
				out["out.txt.error"] = err.Error()
				out["out.txt.correct"] = false
				return out
			}
			word := strings.TrimSpace(string(raw))
			out["out.txt"] = truncate(word, 500)
			out["out.txt.correct"] = strings.EqualFold(word, "garnet")
			return out
		},
	}
}

func probeT2() liveTask {
	return liveTask{
		Name:   "T2",
		Prompt: "Run `go version` and tell me the output. Then run `touch ran.marker` so I know the shell worked.",
		Setup:  func(string) error { return nil },
		Check: func(workspace string) map[string]any {
			out := map[string]any{}
			if _, err := os.Stat(filepath.Join(workspace, "ran.marker")); err == nil {
				out["ran.marker"] = true
			} else {
				out["ran.marker"] = false
				out["ran.marker.error"] = err.Error()
			}
			return out
		},
	}
}

func probeT3() liveTask {
	return liveTask{
		Name:   "T3",
		Prompt: "Run `npx -y cowsay hi` and show the output.",
		Setup:  func(string) error { return nil },
		Check:  func(string) map[string]any { return map[string]any{} },
	}
}

func probeCells() []liveCell {
	ask := func(model string) map[string]any {
		return map[string]any{
			"model":      model,
			"permission": map[string]any{"bash": "ask", "edit": "ask", "webfetch": "ask"},
		}
	}
	defaultConfig := func(model string) map[string]any {
		return map[string]any{"model": model}
	}
	wildcard := func(model string) map[string]any {
		return map[string]any{
			"model":      model,
			"permission": map[string]any{"*": "ask"},
		}
	}
	return []liveCell{
		{ID: "A", Config: defaultConfig, Permit: "allow", Tasks: []liveTask{probeT1(), probeT2()}},
		{ID: "B", Config: ask, Permit: "allow", Tasks: []liveTask{probeT1(), probeT2()}},
		{ID: "C", Config: ask, Permit: "deny", Tasks: []liveTask{probeT1(), probeT2()}},
		{ID: "D", Config: ask, Permit: "allow", Tasks: []liveTask{probeT3()}},
		{ID: "E", Config: wildcard, Permit: "allow", Tasks: []liveTask{probeT1()}},
	}
}

func selectedCells(ids, taskIDs []string) ([]liveCell, error) {
	all := probeCells()
	byID := map[string]liveCell{}
	for _, cell := range all {
		byID[cell.ID] = cell
	}
	var cells []liveCell
	for _, id := range ids {
		cell, ok := byID[strings.ToUpper(strings.TrimSpace(id))]
		if !ok {
			return nil, fmt.Errorf("unknown cell %q (want A,B,C,D,E)", id)
		}
		cells = append(cells, cell)
	}
	if len(cells) == 0 {
		return nil, errors.New("no cells selected")
	}
	wantTasks := map[string]bool{}
	for _, id := range taskIDs {
		id = strings.ToUpper(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		if id != "T1" && id != "T2" && id != "T3" {
			return nil, fmt.Errorf("unknown task %q (want T1,T2,T3)", id)
		}
		wantTasks[id] = true
	}
	if len(wantTasks) > 0 {
		for i := range cells {
			var kept []liveTask
			for _, task := range cells[i].Tasks {
				if wantTasks[task.Name] {
					kept = append(kept, task)
				}
			}
			cells[i].Tasks = kept
		}
	}
	var nonEmpty []liveCell
	for _, cell := range cells {
		if len(cell.Tasks) > 0 {
			nonEmpty = append(nonEmpty, cell)
		}
	}
	if len(nonEmpty) == 0 {
		return nil, errors.New("no cell has a selected task")
	}
	return nonEmpty, nil
}

// livePreflight is the check that the configured free model is the one
// session/new selected, made before any prompt is sent.
