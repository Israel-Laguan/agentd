package worker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"
)

const (
	// ContextPackVersion is the current schema version.
	ContextPackVersion = 1

	// DefaultMaxContextPackPaths is the default cap on workspace paths in a pack.
	DefaultMaxContextPackPaths = 40

	// DefaultMaxContextPackChars is the default cap on total chars across all
	// text fields (summary, excerpt notes, constraints, unknowns).
	DefaultMaxContextPackChars = 48000

	// ContextPackFileName is the workspace file name for a context pack.
	ContextPackFileName = "context_pack.v%d.json"
)

// ContextPack is the sealed handoff artifact produced by the context step.
// It carries everything downstream steps (decision, execute, verify) need
// to operate without broad search. See docs/tiered-execution.md §ContextPack.
type ContextPack struct {
	Version      int              `json:"version"`
	TaskID       string           `json:"task_id"`
	ParentTaskID string           `json:"parent_task_id,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	Summary      string           `json:"summary"`
	Paths        []string         `json:"paths"`
	Excerpts     []ContextExcerpt `json:"excerpts,omitempty"`
	CommandsRun  []CommandRun     `json:"commands_run,omitempty"`
	Constraints  []string         `json:"constraints,omitempty"`
	Unknowns     []string         `json:"unknowns,omitempty"`
	Budget       ContextBudget    `json:"budget"`
}

// ContextExcerpt is a relevant snippet extracted from a workspace file.
type ContextExcerpt struct {
	Path string `json:"path"`
	Note string `json:"note"`
	Span string `json:"span,omitempty"`
}

// CommandRun records a command that was executed during context gathering.
type CommandRun struct {
	Cmd     string `json:"cmd"`
	Outcome string `json:"outcome"`
}

// ContextBudget declares size limits for the pack contents.
type ContextBudget struct {
	MaxPaths  int `json:"max_paths"`
	MaxChars  int `json:"max_chars"`
	PathCount int `json:"path_count"`
	CharCount int `json:"char_count"`
}

// ContextPackConfig holds budget defaults for ContextPack creation.
type ContextPackConfig struct {
	MaxPaths int
	MaxChars int
}

// DefaultContextPackConfig returns the default budget limits.
func DefaultContextPackConfig() ContextPackConfig {
	return ContextPackConfig{
		MaxPaths: DefaultMaxContextPackPaths,
		MaxChars: DefaultMaxContextPackChars,
	}
}

// Validate checks the ContextPack for structural correctness.
func (cp *ContextPack) Validate() error {
	if cp.Version != ContextPackVersion {
		return fmt.Errorf("context pack version %d, want %d", cp.Version, ContextPackVersion)
	}
	if cp.TaskID == "" {
		return fmt.Errorf("context pack task_id is required")
	}
	if cp.Summary == "" {
		return fmt.Errorf("context pack summary is required")
	}
	if len(cp.Paths) == 0 {
		return fmt.Errorf("context pack must have at least one path")
	}
	seen := make(map[string]struct{}, len(cp.Paths))
	for _, p := range cp.Paths {
		if p == "" {
			return fmt.Errorf("context pack path must not be empty")
		}
		if _, dup := seen[p]; dup {
			return fmt.Errorf("context pack duplicate path %q", p)
		}
		seen[p] = struct{}{}
	}
	for i, e := range cp.Excerpts {
		if e.Path == "" {
			return fmt.Errorf("context pack excerpt %d: path is required", i)
		}
	}
	return nil
}

// CharCount returns the total character count across all text fields.
func (cp *ContextPack) CharCount() int {
	n := utf8.RuneCountInString(cp.Summary)
	for _, e := range cp.Excerpts {
		n += utf8.RuneCountInString(e.Note)
		n += utf8.RuneCountInString(e.Span)
	}
	for _, c := range cp.Constraints {
		n += utf8.RuneCountInString(c)
	}
	for _, u := range cp.Unknowns {
		n += utf8.RuneCountInString(u)
	}
	return n
}

// EnforceBudget truncates paths and overflow text to fit the budget.
// Paths beyond max_paths are dropped. Text overflow goes to unknowns.
// Returns the pack (mutated in place) and whether any truncation occurred.
func (cp *ContextPack) EnforceBudget(cfg ContextPackConfig) (truncated bool) {
	if cfg.MaxPaths <= 0 {
		cfg.MaxPaths = DefaultMaxContextPackPaths
	}
	if cfg.MaxChars <= 0 {
		cfg.MaxChars = DefaultMaxContextPackChars
	}
	if len(cp.Paths) > cfg.MaxPaths {
		cp.Paths = cp.Paths[:cfg.MaxPaths]
		truncated = true
	}
	cp.Budget.MaxPaths = cfg.MaxPaths
	cp.Budget.MaxChars = cfg.MaxChars
	cp.Budget.PathCount = len(cp.Paths)
	charCount := cp.CharCount()
	cp.Budget.CharCount = charCount
	if charCount > cfg.MaxChars {
		// Trim unknowns first (they overflow target), then constraints.
		remaining := cfg.MaxChars
		remaining -= utf8.RuneCountInString(cp.Summary)
		for i := range cp.Excerpts {
			remaining -= utf8.RuneCountInString(cp.Excerpts[i].Note)
			remaining -= utf8.RuneCountInString(cp.Excerpts[i].Span)
		}
		for i := range cp.Constraints {
			remaining -= utf8.RuneCountInString(cp.Constraints[i])
		}
		for i := range cp.Unknowns {
			remaining -= utf8.RuneCountInString(cp.Unknowns[i])
		}
		if remaining < 0 {
			// Sum exceeds budget — trim unknowns then constraints.
			cp.Unknowns = nil
			cp.Constraints = nil
			truncated = true
		}
		cp.Budget.CharCount = cp.CharCount()
	}
	return truncated
}

// PackFilePath returns the workspace-relative file path for a context pack
// at the given version.
func PackFilePath(version int) string {
	if version <= 0 {
		version = ContextPackVersion
	}
	return fmt.Sprintf(ContextPackFileName, version)
}

// WriteContextPack serializes the pack to a JSON file in the workspace dir.
func WriteContextPack(workspace string, cp *ContextPack) error {
	if err := cp.Validate(); err != nil {
		return fmt.Errorf("write context pack: %w", err)
	}
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal context pack: %w", err)
	}
	name := PackFilePath(cp.Version)
	path := filepath.Join(workspace, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write context pack %s: %w", name, err)
	}
	return nil
}

// ReadContextPack reads and unmarshals a context pack from a JSON file.
func ReadContextPack(path string) (*ContextPack, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read context pack: %w", err)
	}
	var cp ContextPack
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("unmarshal context pack: %w", err)
	}
	return &cp, nil
}

// NewContextPack creates a minimal valid ContextPack for the given task.
func NewContextPack(taskID, parentTaskID, summary string, paths []string) *ContextPack {
	return &ContextPack{
		Version:      ContextPackVersion,
		TaskID:       taskID,
		ParentTaskID: parentTaskID,
		CreatedAt:    time.Now().UTC(),
		Summary:      summary,
		Paths:        paths,
		Budget: ContextBudget{
			MaxPaths:  DefaultMaxContextPackPaths,
			MaxChars:  DefaultMaxContextPackChars,
			PathCount: len(paths),
		},
	}
}
