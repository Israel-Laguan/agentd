package worker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// Validate checks the ContextPack for structural correctness and
// ensures Budget counters are consistent with the serialized content.
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

// BackfillBudgetCounters fills in budget counters that were absent from a
// serialized v1 pack. Zero is unambiguous here: Validate requires at least one
// path and a non-empty summary, so a valid pack always has a positive
// PathCount and CharCount. Packs written before the counters became mandatory
// (and the v1 sketch in docs/tiered-execution.md) therefore load unmodified.
// Any explicit non-zero counter is left untouched so mismatches still fail
// Validate.
//
// NOTE: this zero-check helper cannot distinguish an absent field from an
// explicit zero. ReadContextPack performs presence-aware backfill instead, so
// explicit zeros reach Validate and are rejected as mismatches. Keep this
// helper for programmatic callers that never serialized an explicit zero.
func (cp *ContextPack) BackfillBudgetCounters() {
	if cp.Budget.PathCount == 0 {
		cp.Budget.PathCount = len(cp.Paths)
	}
	if cp.Budget.CharCount == 0 {
		cp.Budget.CharCount = cp.CharCount()
	}
}

// backfillAbsentBudgetCounters fills only counters whose JSON fields were
// absent from the serialized pack.
func (cp *ContextPack) backfillAbsentBudgetCounters(pathCountPresent, charCountPresent bool) {
	if !pathCountPresent && cp.Budget.PathCount == 0 {
		cp.Budget.PathCount = len(cp.Paths)
	}
	if !charCountPresent && cp.Budget.CharCount == 0 {
		cp.Budget.CharCount = cp.CharCount()
	}
}

// EnforceBudget truncates paths and overflow text to fit the budget.
// Paths beyond max_paths are dropped. Text overflow trims optional fields
// (unknowns, then constraints). CommandsRun, Summary and Excerpts are
// required content counted toward the budget; if they alone exceed MaxChars
// the pack is over budget and an error is returned.
// Returns whether any truncation occurred.
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
	charCount := cp.CharCount()
	cp.Budget.CharCount = charCount
	if charCount > cfg.MaxChars {
		// Compute required content size (summary + excerpts + commands_run).
		required := utf8.RuneCountInString(cp.Summary)
		for i := range cp.Excerpts {
			required += utf8.RuneCountInString(cp.Excerpts[i].Note)
			required += utf8.RuneCountInString(cp.Excerpts[i].Span)
		}
		for i := range cp.CommandsRun {
			required += utf8.RuneCountInString(cp.CommandsRun[i].Cmd)
			required += utf8.RuneCountInString(cp.CommandsRun[i].Outcome)
		}
		if required > cfg.MaxChars {
			cp.Budget.CharCount = charCount
			return truncated, fmt.Errorf("context pack required content %d chars exceeds budget %d (summary+excerpts+commands_run)", required, cfg.MaxChars)
		}
		// Required fits — trim optional content incrementally.
		cp.Unknowns = nil
		cp.Budget.CharCount = cp.CharCount()
		if cp.Budget.CharCount > cfg.MaxChars {
			cp.Constraints = nil
			cp.Budget.CharCount = cp.CharCount()
		}
		truncated = true
	}
	return truncated, nil
}

// PackFilePath returns the workspace-relative file path for a context pack
// at the given version.
func PackFilePath(version int) string {
	v := version
	if v <= 0 {
		v = ContextPackVersion
	}
	return fmt.Sprintf(ContextPackFileName, v)
}

// WriteContextPack serializes the pack to a JSON file in the workspace dir.
func WriteContextPack(workspace string, cp *ContextPack) error {
	cp.Budget.PathCount = len(cp.Paths)
	cp.Budget.CharCount = cp.CharCount()
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
// Budget counters missing from older v1 packs are backfilled before
// validation so previously written packs remain readable. Explicit zero
// counters are preserved so Validate can reject them as mismatches.
func ReadContextPack(path string) (*ContextPack, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read context pack: %w", err)
	}
	var cp ContextPack
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("unmarshal context pack: %w", err)
	}
	var raw struct {
		Budget map[string]json.RawMessage `json:"budget"`
	}
	// Absent until proven present: a legacy pack may omit budget entirely
	// (or send null), in which case both counters must backfill.
	pathCountPresent, charCountPresent := false, false
	if err := json.Unmarshal(data, &raw); err == nil && raw.Budget != nil {
		for k := range raw.Budget {
			switch strings.ToLower(k) {
			case "path_count":
				pathCountPresent = true
			case "char_count":
				charCountPresent = true
			}
		}
	}
	cp.backfillAbsentBudgetCounters(pathCountPresent, charCountPresent)
	if err := cp.Validate(); err != nil {
		return nil, fmt.Errorf("validate context pack: %w", err)
	}
	return &cp, nil
}

// NewContextPack creates a minimal valid ContextPack for the given task.
func NewContextPack(taskID, parentTaskID, summary string, paths []string, cfg ContextPackConfig) *ContextPack {
	if cfg.MaxPaths <= 0 {
		cfg.MaxPaths = DefaultMaxContextPackPaths
	}
	if cfg.MaxChars <= 0 {
		cfg.MaxChars = DefaultMaxContextPackChars
	}
	charCount := 0
	charCount += utf8.RuneCountInString(summary)
	return &ContextPack{
		Version:      ContextPackVersion,
		TaskID:       taskID,
		ParentTaskID: parentTaskID,
		CreatedAt:    time.Now().UTC(),
		Summary:      summary,
		Paths:        paths,
		Budget: ContextBudget{
			MaxPaths:  cfg.MaxPaths,
			MaxChars:  cfg.MaxChars,
			PathCount: len(paths),
			CharCount: charCount,
		},
	}
}
