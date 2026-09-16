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

	// DefaultMaxContextPackChars is the default cap on total chars across text fields.
	DefaultMaxContextPackChars = 48000

	// ContextPackFileName is the workspace file name for a context pack.
	ContextPackFileName = "context_pack.v%d.json"
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

func stringLen(s string) int {
	return utf8.RuneCountInString(s)
}

// CharCount returns the total character count across all text fields.
func (cp *ContextPack) CharCount() int {
	n := stringLen(cp.Summary)
	for _, e := range cp.Excerpts {
		n += stringLen(e.Note)
		n += stringLen(e.Span)
	}
	for _, cr := range cp.CommandsRun {
		n += stringLen(cr.Cmd)
		n += stringLen(cr.Outcome)
	}
	for _, c := range cp.Constraints {
		n += stringLen(c)
	}
	for _, u := range cp.Unknowns {
		n += stringLen(u)
	}
	return n
}

// BackfillBudgetCounters fills zero-valued budget counters from serialized content.
func (cp *ContextPack) BackfillBudgetCounters() {
	if cp.Budget.PathCount == 0 {
		cp.Budget.PathCount = len(cp.Paths)
	}
	if cp.Budget.CharCount == 0 {
		cp.Budget.CharCount = cp.CharCount()
	}
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

// requiredContentChars returns the char count of required fields.
func (cp *ContextPack) requiredContentChars() int {
	n := stringLen(cp.Summary)
	for i := range cp.Excerpts {
		n += stringLen(cp.Excerpts[i].Note)
		n += stringLen(cp.Excerpts[i].Span)
	}
	for i := range cp.CommandsRun {
		n += stringLen(cp.CommandsRun[i].Cmd)
		n += stringLen(cp.CommandsRun[i].Outcome)
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
	cp.Budget.CharCount = cp.CharCount()
	if cp.Budget.CharCount > cfg.MaxChars {
		required := cp.requiredContentChars()
		if required > cfg.MaxChars {
			return truncated, fmt.Errorf("context pack required content %d chars exceeds budget %d (summary+excerpts+commands_run)", required, cfg.MaxChars)
		}
		// Required fits — trim optional content incrementally so clearing
		// Unknowns alone can preserve safety-critical Constraints.
		cp.Unknowns = nil
		cp.Budget.CharCount = cp.CharCount()
		if cp.Budget.CharCount > cfg.MaxChars {
			cp.Constraints = nil
			cp.Budget.CharCount = cp.CharCount()
		}
		truncated = true
	}
	cp.Budget.CharCount = cp.CharCount()
	return truncated, nil
}

// PackFilePath returns the workspace-relative file path for a context pack at the given version.
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
// validation so previously written packs remain readable. Explicit
// zero counters are preserved so Validate can reject them.
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
			CharCount: stringLen(summary),
		},
	}
}
