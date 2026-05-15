package worker

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Skill — parsed representation of a single skill file
// ---------------------------------------------------------------------------

// Skill is the parsed representation of a single skill markdown file.
// Skills are knowledge documents describing how to perform a category of
// task. They are injected into context on demand, not loaded wholesale.
type Skill struct {
	Name           string // extracted from "# Skill: <name>"
	WhenApplies    string // body of "## When This Applies"
	Procedure      string // body of "## The Procedure"
	CommonMistakes string // body of "## Common Mistakes"
	OutputFormat   string // body of "## Output Format"
	Raw            string // full original markdown content
	SourcePath     string // filesystem path the skill was loaded from
}

// IsEmpty returns true when the skill has no meaningful content.
func (s *Skill) IsEmpty() bool {
	return s == nil || (s.Name == "" && s.Raw == "")
}

// parseSkillMD splits a skill markdown document into structured sections.
// The expected format uses a top-level H1 "# Skill: <name>" and H2 sections
// for each field.
func parseSkillMD(content, sourcePath string) *Skill {
	sk := &Skill{Raw: content, SourcePath: sourcePath}
	if content == "" {
		return sk
	}

	sections := splitH2Sections(content)

	// Extract skill name from the H1 heading.
	sk.Name = extractSkillName(content)

	for heading, body := range sections {
		normalized := strings.ToLower(strings.TrimSpace(heading))
		switch normalized {
		case "when this applies":
			sk.WhenApplies = strings.TrimSpace(body)
		case "the procedure":
			sk.Procedure = strings.TrimSpace(body)
		case "common mistakes":
			sk.CommonMistakes = strings.TrimSpace(body)
		case "output format":
			sk.OutputFormat = strings.TrimSpace(body)
		}
	}
	return sk
}

// extractSkillName finds the first H1 heading matching "# Skill: <name>"
// and returns <name>. Falls back to the first H1 text if no prefix match.
func extractSkillName(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "# ") {
			continue
		}
		heading := strings.TrimPrefix(trimmed, "# ")
		if after, ok := strings.CutPrefix(heading, "Skill: "); ok {
			return strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(heading, "Skill:"); ok {
			return strings.TrimSpace(after)
		}
		return strings.TrimSpace(heading)
	}
	return ""
}

// ---------------------------------------------------------------------------
// SkillLoader — reads skill files from project and global directories
// ---------------------------------------------------------------------------

// SkillLoader reads and parses skill markdown files from the filesystem.
type SkillLoader struct {
	// ProjectDir is the relative path within a workspace for project-scoped
	// skills (e.g., ".agentd/skills/").
	ProjectDir string

	// GlobalDir is the absolute path to the global skills directory
	// (resolved by config before the worker runs; may still start with "~/" when constructed in tests).
	GlobalDir string
}

// LoadAll loads skills from both the project directory and the global
// directory. Project skills take precedence over global skills with the
// same name (by replacing the global entry).
func (l *SkillLoader) LoadAll(workspacePath string) ([]*Skill, error) {
	seen := make(map[string]int) // name → index in result slice
	var skills []*Skill

	// Load project-scoped skills first (higher precedence).
	if workspacePath != "" && l.ProjectDir != "" {
		dir := filepath.Join(workspacePath, l.ProjectDir)
		loaded, err := l.loadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("load project skills: %w", err)
		}
		for _, sk := range loaded {
			seen[sk.Name] = len(skills)
			skills = append(skills, sk)
		}
	}

	// Load global skills; skip any already loaded from project scope.
	if l.GlobalDir != "" {
		loaded, err := l.loadDir(l.GlobalDir)
		if err != nil {
			return nil, fmt.Errorf("load global skills: %w", err)
		}
		for _, sk := range loaded {
			if _, exists := seen[sk.Name]; exists {
				slog.Debug("global skill shadowed by project skill", "name", sk.Name)
				continue
			}
			seen[sk.Name] = len(skills)
			skills = append(skills, sk)
		}
	}

	slog.Debug("loaded skills", "count", len(skills))
	return skills, nil
}

// loadDir reads all .md files from dir, parses each as a skill, and returns
// non-empty skills. Returns (nil, nil) when the directory does not exist.
func (l *SkillLoader) loadDir(dir string) ([]*Skill, error) {
	expanded := dir
	if strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			expanded = filepath.Join(home, dir[2:])
		}
	}
	entries, err := os.ReadDir(expanded)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read skill directory %s: %w", expanded, err)
	}

	var skills []*Skill
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			slog.Warn("skipping symlinked skill file", "path", filepath.Join(expanded, entry.Name()))
			continue
		}
		path := filepath.Join(expanded, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("skipping unreadable skill file", "path", path, "error", err)
			continue
		}
		sk := parseSkillMD(string(data), path)
		if sk.IsEmpty() {
			continue
		}
		skills = append(skills, sk)
	}
	return skills, nil
}

// ---------------------------------------------------------------------------
// SkillRouter — matches tasks to relevant skills via TF-IDF scoring
// ---------------------------------------------------------------------------

// SkillRouter selects the most relevant skills for a given task description
// using TF-IDF scoring against each skill's "When This Applies" section.
type SkillRouter struct {
	// TopK is the maximum number of skills to return (default 3).
	TopK int

	// Threshold is the minimum relevance score for a skill to be included.
	// Skills scoring below this are excluded even if within TopK.
	Threshold float64
}

// scoredSkill pairs a skill with its computed relevance score.
type scoredSkill struct {
	skill *Skill
	score float64
}

// Match selects the most relevant skills for the given task description.
// It uses TF-IDF cosine similarity between the task text and each skill's
// WhenApplies section. Returns at most TopK skills above the Threshold.
func (r *SkillRouter) Match(taskDescription string, skills []*Skill) []*Skill {
	if len(skills) == 0 || taskDescription == "" {
		return nil
	}

	topK := r.TopK
	if topK <= 0 {
		topK = 3
	}
	threshold := r.Threshold

	// Filter out nil entries to prevent panics.
	filtered := make([]*Skill, 0, len(skills))
	for _, sk := range skills {
		if sk != nil {
			filtered = append(filtered, sk)
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	// Build corpus: task description + all non-nil WhenApplies sections.
	corpus := make([]string, 0, len(filtered)+1)
	corpus = append(corpus, taskDescription)
	for _, sk := range filtered {
		corpus = append(corpus, sk.WhenApplies)
	}

	tfidf := newTFIDF(corpus)

	// Score each skill against the task description.
	taskVec := tfidf.vector(0)
	var scored []scoredSkill
	for i, sk := range filtered {
		docVec := tfidf.vector(i + 1)
		sim := cosineSimilarity(taskVec, docVec)
		if sim >= threshold {
			scored = append(scored, scoredSkill{skill: sk, score: sim})
		}
	}

	// Sort by score descending.
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Trim to topK.
	if len(scored) > topK {
		scored = scored[:topK]
	}

	result := make([]*Skill, len(scored))
	for i, s := range scored {
		result[i] = s.skill
	}
	return result
}

// FormatSkillBlock renders a matched skill as a prompt section suitable
// for injection into the system prompt.
func FormatSkillBlock(sk *Skill) string {
	if sk == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "=== Skill: %s ===\n", sk.Name)
	if sk.WhenApplies != "" {
		fmt.Fprintf(&b, "When This Applies:\n%s\n\n", sk.WhenApplies)
	}
	if sk.Procedure != "" {
		fmt.Fprintf(&b, "Procedure:\n%s\n\n", sk.Procedure)
	}
	if sk.CommonMistakes != "" {
		fmt.Fprintf(&b, "Common Mistakes:\n%s\n\n", sk.CommonMistakes)
	}
	if sk.OutputFormat != "" {
		fmt.Fprintf(&b, "Output Format:\n%s\n\n", sk.OutputFormat)
	}
	return strings.TrimRight(b.String(), "\n")
}
