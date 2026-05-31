package runtime

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"agentd/internal/models"
)

//go:embed prompt_templates.json
var embeddedPromptTemplates []byte

const (
	TemplateCodePromptBuilder = "CODE_PROMPT_BUILDER"

	slotLanguage  = "language"
	slotFilename  = "filename"
	slotSignature = "signature"
	slotTestCases = "test_cases"
)

// PromptTemplate is a named prompt with typed slots.
type PromptTemplate struct {
	System        string   `json:"system"`
	UserPrompt    string   `json:"user_prompt"`
	RequiredSlots []string `json:"required_slots"`
}

// RenderSession carries per-session context for template rendering.
type RenderSession struct {
	SystemPrefix string
}

// RenderedPrompt is the substituted system and user messages.
type RenderedPrompt struct {
	System string
	User   string
}

// PromptLibrary stores named templates loaded from embed and optional disk path.
type PromptLibrary struct {
	path      string
	templates map[string]PromptTemplate
	mu        sync.RWMutex
}

// NewPromptLibrary loads embedded defaults and merges templates from path when the file exists.
func NewPromptLibrary(path string) (*PromptLibrary, error) {
	templates, err := parsePromptTemplatesJSON(embeddedPromptTemplates)
	if err != nil {
		return nil, fmt.Errorf("parse embedded prompt templates: %w", err)
	}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read prompt templates %s: %w", path, err)
		}
		if err == nil {
			fromDisk, err := parsePromptTemplatesJSON(data)
			if err != nil {
				return nil, fmt.Errorf("parse prompt templates %s: %w", path, err)
			}
			for name, tmpl := range fromDisk {
				templates[name] = tmpl
			}
		}
	}
	return &PromptLibrary{path: path, templates: templates}, nil
}

func parsePromptTemplatesJSON(data []byte) (map[string]PromptTemplate, error) {
	var raw map[string]PromptTemplate
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return make(map[string]PromptTemplate), nil
	}
	return raw, nil
}

// Render substitutes slots and prepends session.SystemPrefix to the template system prompt.
func (lib *PromptLibrary) Render(name string, session RenderSession, slots map[string]string) (RenderedPrompt, error) {
	lib.mu.RLock()
	tmpl, ok := lib.templates[name]
	lib.mu.RUnlock()
	if !ok {
		return RenderedPrompt{}, fmt.Errorf("prompt template %q: not found", name)
	}
	for _, slot := range tmpl.RequiredSlots {
		val, present := slots[slot]
		if !present || strings.TrimSpace(val) == "" {
			return RenderedPrompt{}, fmt.Errorf("prompt template %q: missing required slot %q", name, slot)
		}
	}
	system := substituteSlots(tmpl.System, slots)
	user := substituteSlots(tmpl.UserPrompt, slots)
	finalSystem := joinSystemPrefix(session.SystemPrefix, system)
	return RenderedPrompt{System: finalSystem, User: user}, nil
}

// Save registers or replaces a template and persists the full library to disk when path is set.
func (lib *PromptLibrary) Save(name string, tmpl PromptTemplate) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("prompt template: empty name")
	}
	lib.mu.Lock()
	defer lib.mu.Unlock()
	if lib.path == "" {
		lib.templates[name] = tmpl
		return nil
	}
	toPersist := make(map[string]PromptTemplate, len(lib.templates)+1)
	for k, v := range lib.templates {
		toPersist[k] = v
	}
	toPersist[name] = tmpl
	if err := writePromptTemplatesFile(lib.path, toPersist); err != nil {
		return err
	}
	lib.templates[name] = tmpl
	return nil
}

func writePromptTemplatesFile(path string, templates map[string]PromptTemplate) error {
	data, err := json.MarshalIndent(templates, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal prompt templates: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create prompt templates dir: %w", err)
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp prompt templates: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp prompt templates: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp prompt templates: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp prompt templates: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename prompt templates: %w", err)
	}
	return nil
}

func substituteSlots(text string, slots map[string]string) string {
	return slotRegex.ReplaceAllStringFunc(text, func(m string) string {
		key := m[2 : len(m)-2]
		if val, ok := slots[key]; ok {
			return val
		}
		return m
	})
}

func joinSystemPrefix(prefix, templateSystem string) string {
	prefix = strings.TrimSpace(prefix)
	templateSystem = strings.TrimSpace(templateSystem)
	switch {
	case prefix == "":
		return templateSystem
	case templateSystem == "":
		return prefix
	default:
		return prefix + "\n\n" + templateSystem
	}
}

var (
	pathLikeToken         = regexp.MustCompile(`(?i)[\w./-]+\.(go|py|ts|tsx|js|jsx|rs|java|rb|php|cs|cpp|c|h|swift|kt|scala|sh|sql|yaml|yml|json|md)`)
	slotRegex             = regexp.MustCompile(`\{\{([^}]+)\}\}`)
	promptSectionHeaderRE = regexp.MustCompile(`(?im)^\s*(language|filename|signature|test cases|tests)\s*:`)
	extToLanguage         = map[string]string{
		"go": "go", "py": "python", "ts": "typescript", "tsx": "typescript",
		"js": "javascript", "jsx": "javascript", "rs": "rust", "java": "java",
		"rb": "ruby", "php": "php", "cs": "csharp", "cpp": "cpp", "c": "c",
		"h": "c", "swift": "swift", "kt": "kotlin", "scala": "scala",
		"sh": "shell", "sql": "sql", "yaml": "yaml", "yml": "yaml",
		"json": "json", "md": "markdown",
	}
)

// buildCodeGenSlots extracts slot values from a task for CODE_PROMPT_BUILDER.
func buildCodeGenSlots(task models.Task) map[string]string {
	text := task.Title + "\n" + task.Description
	lang := detectCodeLanguage(text)
	filename := detectCodeFilename(text, task.Title)
	signature := extractPromptSection(text, "Signature:", task.Description)
	testCases := extractPromptTestCases(text, "None specified.")
	return map[string]string{
		slotLanguage:  lang,
		slotFilename:  filename,
		slotSignature: signature,
		slotTestCases: testCases,
	}
}

func detectCodeLanguage(text string) string {
	if m := pathLikeToken.FindString(text); m != "" {
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(m), "."))
		if lang, ok := extToLanguage[ext]; ok {
			return lang
		}
		return ext
	}
	return "go"
}

func detectCodeFilename(text, title string) string {
	if m := pathLikeToken.FindString(text); m != "" {
		return filepath.Base(m)
	}
	slug := strings.ToLower(strings.TrimSpace(title))
	slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "_")
	slug = strings.Trim(slug, "_")
	if slug == "" {
		slug = "output"
	}
	return slug + ".go"
}

func extractPromptSection(text, header, fallback string) string {
	lower := strings.ToLower(text)
	h := strings.ToLower(header)
	idx := strings.Index(lower, h)
	if idx < 0 {
		return strings.TrimSpace(fallback)
	}
	body := strings.TrimLeft(text[idx+len(header):], " \t\r\n")
	if loc := promptSectionHeaderRE.FindStringIndex(body); loc != nil {
		body = body[:loc[0]]
	}
	return strings.TrimSpace(body)
}

func extractPromptTestCases(text, fallback string) string {
	if v := extractPromptSection(text, "Test cases:", ""); v != "" {
		return v
	}
	if v := extractPromptSection(text, "Tests:", ""); v != "" {
		return v
	}
	return fallback
}

// BuildCodeGenSlots extracts slot values from a task for CODE_PROMPT_BUILDER.
func BuildCodeGenSlots(task models.Task) map[string]string {
	return buildCodeGenSlots(task)
}
