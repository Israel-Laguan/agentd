package worker

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Instruction level enum
// ---------------------------------------------------------------------------

// InstructionLevel represents the scope of an instruction layer.
type InstructionLevel int

const (
	// LevelGlobal is the harness-level instruction layer: general agent
	// behavior, output format, meta-instructions. Stable across sessions.
	LevelGlobal InstructionLevel = iota

	// LevelUserPreference is the user-level instruction layer loaded from
	// a persistent profile (e.g., ~/.agentd/prefs.yaml). Preferences follow
	// the user across projects and are silently prepended.
	LevelUserPreference

	// LevelProject is the repository-level instruction layer loaded from
	// a convention file such as <workspace>/.agentd/AGENTS.md.
	LevelProject

	// LevelTask is the session-level instruction layer injected from the
	// caller's task description and any AgentProfile overrides.
	LevelTask
)

// ---------------------------------------------------------------------------
// Parsed instruction structures
// ---------------------------------------------------------------------------

// ProjectInstructions holds the parsed sections from a project-level
// AGENTS.md file. Each field corresponds to a Markdown H2 section heading.
type ProjectInstructions struct {
	Architecture string // ## Architecture
	Conventions  string // ## Conventions
	KnownHazards string // ## Known Hazards
	AgentScope   string // ## Agent Scope
	Raw          string // full original content
}

// IsEmpty returns true when no section has content.
func (pi *ProjectInstructions) IsEmpty() bool {
	if pi == nil {
		return true
	}
	return pi.Architecture == "" && pi.Conventions == "" &&
		pi.KnownHazards == "" && pi.AgentScope == "" && pi.Raw == ""
}

// UserPreferences holds key-value pairs loaded from the user preferences
// YAML file. These are serialized into prompt text and silently prepended.
type UserPreferences struct {
	Entries map[string]string `yaml:"preferences"`
}

// IsEmpty returns true when no preferences are defined.
func (up *UserPreferences) IsEmpty() bool {
	return up == nil || len(up.Entries) == 0
}

// FormatPrompt renders preferences as a prompt-friendly text block.
func (up *UserPreferences) FormatPrompt() string {
	if up.IsEmpty() {
		return ""
	}
	var b strings.Builder
	b.WriteString("User Preferences (apply to every response):\n")
	for k, v := range up.Entries {
		fmt.Fprintf(&b, "- %s: %s\n", k, v)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// InstructionLoader — reads and parses instruction files
// ---------------------------------------------------------------------------

// InstructionLoader reads instruction files from the filesystem.
type InstructionLoader struct {
	// ProjectFile is the default relative path within a workspace.
	// e.g., ".agentd/AGENTS.md"
	ProjectFile string

	// UserPreferencesPath is the absolute path to the user preferences file.
	// e.g., "/home/user/.agentd/prefs.yaml"
	UserPreferencesPath string
}

// LoadProjectInstructions reads and parses a project-level instruction file.
// It first tries <workspace>/<overridePath> (when overridePath is non-empty),
// then <workspace>/<loader.ProjectFile>, then <workspace>/AGENTS.md as a
// fallback. Returns (nil, nil) if no instruction file exists.
func (l *InstructionLoader) LoadProjectInstructions(workspacePath, overridePath string) (*ProjectInstructions, error) {
	candidates := l.instructionCandidates(workspacePath, overridePath)
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read project instructions %s: %w", path, err)
		}
		slog.Debug("loaded project instructions", "path", path)
		pi := parseAgentsMD(string(data))
		return pi, nil
	}
	return nil, nil
}

// instructionCandidates returns an ordered list of candidate file paths
// for project instructions. The first existing file wins.
func (l *InstructionLoader) instructionCandidates(workspacePath, overridePath string) []string {
	var candidates []string
	if overridePath != "" {
		candidates = append(candidates, filepath.Join(workspacePath, overridePath))
	}
	if l.ProjectFile != "" {
		candidates = append(candidates, filepath.Join(workspacePath, l.ProjectFile))
	}
	// Fallback: AGENTS.md at workspace root
	candidates = append(candidates, filepath.Join(workspacePath, "AGENTS.md"))
	return candidates
}

// LoadUserPreferences reads user preferences from the configured YAML path.
// Returns (nil, nil) if the file does not exist.
func (l *InstructionLoader) LoadUserPreferences() (*UserPreferences, error) {
	if l.UserPreferencesPath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(l.UserPreferencesPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read user preferences %s: %w", l.UserPreferencesPath, err)
	}
	var prefs UserPreferences
	if err := yaml.Unmarshal(data, &prefs); err != nil {
		return nil, fmt.Errorf("parse user preferences %s: %w", l.UserPreferencesPath, err)
	}
	slog.Debug("loaded user preferences", "path", l.UserPreferencesPath, "count", len(prefs.Entries))
	return &prefs, nil
}

// ---------------------------------------------------------------------------
// AGENTS.md parser
// ---------------------------------------------------------------------------

// parseAgentsMD splits a Markdown document into sections by H2 headings.
// Known headings are mapped to named fields; the full content is stored in Raw.
func parseAgentsMD(content string) *ProjectInstructions {
	pi := &ProjectInstructions{Raw: content}
	if content == "" {
		return pi
	}

	sections := splitH2Sections(content)
	for heading, body := range sections {
		normalized := strings.ToLower(strings.TrimSpace(heading))
		switch normalized {
		case "architecture":
			pi.Architecture = strings.TrimSpace(body)
		case "conventions":
			pi.Conventions = strings.TrimSpace(body)
		case "known hazards":
			pi.KnownHazards = strings.TrimSpace(body)
		case "agent scope":
			pi.AgentScope = strings.TrimSpace(body)
		}
	}
	return pi
}

// splitH2Sections parses markdown text and returns a map of H2 heading → body.
func splitH2Sections(content string) map[string]string {
	sections := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))

	currentHeading := ""
	var currentBody strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "## ") {
			// Flush previous section
			if currentHeading != "" {
				sections[currentHeading] = currentBody.String()
			}
			currentHeading = strings.TrimPrefix(line, "## ")
			currentBody.Reset()
		} else if currentHeading != "" {
			currentBody.WriteString(line)
			currentBody.WriteString("\n")
		}
	}
	// Flush last section
	if currentHeading != "" {
		sections[currentHeading] = currentBody.String()
	}
	return sections
}

// ---------------------------------------------------------------------------
// SystemPromptBuilder — assembles the final prompt with explicit precedence
// ---------------------------------------------------------------------------

// resolutionRule is appended to every assembled prompt so the model knows
// how to resolve conflicting instructions across layers.
const resolutionRule = "If instructions at different levels conflict, task-level overrides project-level which overrides global."

// SystemPromptBuilder assembles the final system prompt by layering
// instructions from multiple scopes in a deterministic order.
type SystemPromptBuilder struct {
	global      string
	project     *ProjectInstructions
	taskPrompt  string
	userPrefs   *UserPreferences
}

// NewSystemPromptBuilder creates a builder with no layers set.
func NewSystemPromptBuilder() *SystemPromptBuilder {
	return &SystemPromptBuilder{}
}

// WithGlobal sets the global-level instructions (e.g., agentic tool-use text).
func (b *SystemPromptBuilder) WithGlobal(content string) *SystemPromptBuilder {
	b.global = content
	return b
}

// WithProject sets the project-level instructions from a parsed AGENTS.md.
func (b *SystemPromptBuilder) WithProject(instructions *ProjectInstructions) *SystemPromptBuilder {
	b.project = instructions
	return b
}

// WithTask sets the task-level instructions from AgentProfile.SystemPrompt.
func (b *SystemPromptBuilder) WithTask(prompt string) *SystemPromptBuilder {
	b.taskPrompt = prompt
	return b
}

// WithUserPreferences sets user preferences to prepend silently.
func (b *SystemPromptBuilder) WithUserPreferences(prefs *UserPreferences) *SystemPromptBuilder {
	b.userPrefs = prefs
	return b
}

// Build assembles the final system prompt in precedence order:
//  1. User preferences (silent prefix)
//  2. Known Hazards (safety prefix, from project layer)
//  3. Global (agentic tool-use system text)
//  4. Agent Scope (capability constraints, from project layer)
//  5. Task-level SystemPrompt (highest precedence override)
//  6. Architecture + Conventions (project context)
//  7. Resolution rule
func (b *SystemPromptBuilder) Build() string {
	var sections []string

	// 1. User preferences — always first, silent prefix
	if !b.userPrefs.IsEmpty() {
		if text := b.userPrefs.FormatPrompt(); text != "" {
			sections = append(sections, text)
		}
	}

	// 2. Known Hazards — safety-first positioning
	if b.project != nil && b.project.KnownHazards != "" {
		sections = append(sections, "KNOWN HAZARDS (from project):\n"+b.project.KnownHazards)
	}

	// 3. Global agentic instructions
	if b.global != "" {
		sections = append(sections, b.global)
	}

	// 4. Agent Scope — capability constraints
	if b.project != nil && b.project.AgentScope != "" {
		sections = append(sections, "AGENT SCOPE (from project):\n"+b.project.AgentScope)
	}

	// 5. Architecture + Conventions — project context
	if b.project != nil && b.project.Architecture != "" {
		sections = append(sections, "ARCHITECTURE (from project):\n"+b.project.Architecture)
	}
	if b.project != nil && b.project.Conventions != "" {
		sections = append(sections, "CONVENTIONS (from project):\n"+b.project.Conventions)
	}

	// 6. Task-level override — highest precedence content
	if b.taskPrompt != "" {
		sections = append(sections, b.taskPrompt)
	}

	// 7. Resolution rule — always appended
	sections = append(sections, resolutionRule)

	return strings.Join(sections, "\n\n")
}
