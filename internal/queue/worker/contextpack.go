package worker

import (
	"fmt"
	"time"
	"unicode/utf8"
)

const (
	// ContextPackVersion is the current schema version.
	ContextPackVersion = 1

	// DefaultMaxContextPackPaths is the default cap on workspace paths in a pack.
	DefaultMaxContextPackPaths = 40

	// DefaultMaxContextPackChars is the default cap on total chars across text fields.
	DefaultMaxContextPackChars = 48000

	// ContextPackFileName is the workspace file name template for a context
	// pack, scoped by the tiered pipeline's origin (parent) task so concurrent
	// pipelines sharing a project workspace cannot overwrite each other's pack.
	ContextPackFileName = "context_pack.%s.v%d.json"

	// legacyContextPackFileName is the unscoped file name used when a pack
	// carries no parent-task lineage.
	legacyContextPackFileName = "context_pack.v%d.json"
)

// ContextPack is the sealed handoff artifact produced by the context step.
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

// ContextExcerpt is a relevant snippet from a workspace file.
type ContextExcerpt struct {
	Path string `json:"path"`
	Note string `json:"note"`
	Span string `json:"span,omitempty"`
}

// CommandRun records a command executed during context gathering.
type CommandRun struct {
	Cmd     string `json:"cmd"`
	Outcome string `json:"outcome"`
}

// ContextBudget declares size limits for pack contents.
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

// Validate checks the ContextPack for structural correctness and budget counter consistency.
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
	if cp.Budget.PathCount != len(cp.Paths) {
		return fmt.Errorf("context pack budget path_count %d does not match paths len %d", cp.Budget.PathCount, len(cp.Paths))
	}
	if cp.Budget.CharCount != cp.CharCount() {
		return fmt.Errorf("context pack budget char_count %d does not match computed char count %d", cp.Budget.CharCount, cp.CharCount())
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
	for _, cr := range cp.CommandsRun {
		n += utf8.RuneCountInString(cr.Cmd)
		n += utf8.RuneCountInString(cr.Outcome)
	}
	for _, c := range cp.Constraints {
		n += utf8.RuneCountInString(c)
	}
	for _, u := range cp.Unknowns {
		n += utf8.RuneCountInString(u)
	}
	return n
}

// backfillAbsentBudgetCounters fills counters absent from the serialized pack.
func (cp *ContextPack) backfillAbsentBudgetCounters(pathCountPresent, charCountPresent bool) {
	if !pathCountPresent && cp.Budget.PathCount == 0 {
		cp.Budget.PathCount = len(cp.Paths)
	}
	if !charCountPresent && cp.Budget.CharCount == 0 {
		cp.Budget.CharCount = cp.CharCount()
	}
}

// requiredContentChars returns the char count of the fields EnforceBudget
// cannot drop: Summary, Excerpts and CommandsRun. Constraints and Unknowns are
// optional and deliberately excluded — EnforceBudget clears them to bring
// CharCount() back under MaxChars, so counting them here would reject packs
// that trimming can still fit.
func (cp *ContextPack) requiredContentChars() int {
	n := utf8.RuneCountInString(cp.Summary)
	for i := range cp.Excerpts {
		n += utf8.RuneCountInString(cp.Excerpts[i].Note)
		n += utf8.RuneCountInString(cp.Excerpts[i].Span)
	}
	for i := range cp.CommandsRun {
		n += utf8.RuneCountInString(cp.CommandsRun[i].Cmd)
		n += utf8.RuneCountInString(cp.CommandsRun[i].Outcome)
	}
	return n
}

// EnforceBudget truncates paths and overflow text to fit the budget.
// Paths beyond max_paths are dropped. Text overflow trims optional
// fields (unknowns, then constraints). CommandsRun, Summary and
// Excerpts are required content; if they alone exceed MaxChars an
// error is returned. Returns whether any truncation occurred.
func (cp *ContextPack) EnforceBudget(cfg ContextPackConfig) (truncated bool, err error) {
	if cfg.MaxPaths <= 0 {
		cfg.MaxPaths = DefaultMaxContextPackPaths
	}
	if cfg.MaxChars <= 0 {
		cfg.MaxChars = DefaultMaxContextPackChars
	}
	if len(cp.Paths) > cfg.MaxPaths {
		cp.Paths = cp.Paths[:cfg.MaxPaths]
		allowed := make(map[string]struct{}, len(cp.Paths))
		for _, p := range cp.Paths {
			allowed[p] = struct{}{}
		}
		kept := cp.Excerpts[:0]
		for _, e := range cp.Excerpts {
			if _, ok := allowed[e.Path]; ok {
				kept = append(kept, e)
			}
		}
		cp.Excerpts = kept
		truncated = true
	}
	cp.Budget.MaxPaths = cfg.MaxPaths
	cp.Budget.MaxChars = cfg.MaxChars
	cp.Budget.PathCount = len(cp.Paths)
	// Count once and store immediately so the budget stays consistent even on
	// the error return below.
	charCount := cp.CharCount()
	cp.Budget.CharCount = charCount
	if charCount > cfg.MaxChars {
		required := cp.requiredContentChars()
		if required > cfg.MaxChars {
			return truncated, fmt.Errorf("context pack required content %d chars exceeds budget %d (summary+excerpts+commands_run)", required, cfg.MaxChars)
		}
		// Required fits — trim optional content incrementally so clearing
		// Unknowns alone can preserve safety-critical Constraints.
		cp.Unknowns = nil
		charCount = cp.CharCount()
		if charCount > cfg.MaxChars {
			cp.Constraints = nil
			charCount = cp.CharCount()
		}
		truncated = true
	}
	cp.Budget.CharCount = charCount
	return truncated, nil
}
