package worker

import (
	"log/slog"
	"regexp"
	"strings"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// Task type categories for tool manifest filtering.
const (
	TaskTypeSummarize    = "summarize"
	TaskTypeCodeGen      = "code_gen"
	TaskTypeDocQA        = "doc_qa"
	TaskTypeWebResearch  = "web_research"
	TaskTypeFullAgent    = "full_agent"
	toolManifestWildcard = "*"
)

var (
	summarizeSignals   = []string{"summarize", "summary", "recap", "condense", "tl;dr", "tldr"}
	docQASignals       = []string{"explain", "what is", "how does", "document", "readme", "describe"}
	codeGenSignals     = []string{"implement", "fix", "refactor", "add", "create", "patch", "test", "build"}
	webResearchSignals = []string{"search", "fetch", "url", "web", "browse", "lookup"}
	fullAgentSignals   = []string{"deploy", "migrate", "orchestrate", "multi-step", "delegate"}

	taskManifestSignals map[string][]string
	taskManifestMatchers map[string]map[string]*regexp.Regexp
)

func init() {
	taskManifestSignals = map[string][]string{
		TaskTypeSummarize:   summarizeSignals,
		TaskTypeDocQA:       docQASignals,
		TaskTypeCodeGen:     codeGenSignals,
		TaskTypeWebResearch: webResearchSignals,
		TaskTypeFullAgent:   fullAgentSignals,
	}
	taskManifestMatchers = make(map[string]map[string]*regexp.Regexp, len(taskManifestSignals))
	for taskType, keywords := range taskManifestSignals {
		matchers := make(map[string]*regexp.Regexp, len(keywords))
		for _, kw := range keywords {
			matchers[kw] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(kw) + `\b`)
		}
		taskManifestMatchers[taskType] = matchers
	}
}

// TaskClassification is the output of keyword-based task classification.
type TaskClassification struct {
	Type       string
	Confidence float64
	Scores     map[string]int
}

// TaskClassifier scores task text into manifest categories.
type TaskClassifier struct {
	minConfidence float64
}

// NewTaskClassifier returns a classifier with the given minimum confidence threshold.
func NewTaskClassifier(minConfidence float64) *TaskClassifier {
	return &TaskClassifier{minConfidence: minConfidence}
}

// Classify scores task title and description into a task type with confidence.
func (c *TaskClassifier) Classify(task models.Task) TaskClassification {
	text := strings.ToLower(task.Title + " " + task.Description)
	scores := make(map[string]int, len(taskManifestSignals))
	for taskType, keywords := range taskManifestSignals {
		for _, kw := range keywords {
			scores[taskType] += countTaskManifestKeyword(text, taskType, kw)
		}
	}

	topType, topScore, secondScore := topTwoScores(scores)
	confidence := 0.0
	if topScore > 0 {
		confidence = float64(topScore-secondScore) / float64(topScore)
		if confidence > 1 {
			confidence = 1
		}
	}

	if topScore == 0 || confidence < c.minConfidence {
		return TaskClassification{
			Type:       TaskTypeFullAgent,
			Confidence: confidence,
			Scores:     scores,
		}
	}
	return TaskClassification{
		Type:       topType,
		Confidence: confidence,
		Scores:     scores,
	}
}

func countTaskManifestKeyword(text, taskType, kw string) int {
	matchers := taskManifestMatchers[taskType]
	if matchers == nil {
		return 0
	}
	re := matchers[kw]
	if re == nil {
		return 0
	}
	return len(re.FindAllStringIndex(text, -1))
}

func topTwoScores(scores map[string]int) (topType string, top, second int) {
	for taskType, score := range scores {
		if score > top {
			second = top
			top = score
			topType = taskType
		} else if score > second {
			second = score
		}
	}
	if topType == "" {
		topType = TaskTypeFullAgent
	}
	return topType, top, second
}

// ToolManifest filters gateway tool definitions by task type and profile overrides.
type ToolManifest struct {
	cfg      config.ToolManifestConfig
	mappings map[string][]string
	classifier *TaskClassifier
}

// NewToolManifest returns a manifest when enabled; otherwise nil.
func NewToolManifest(cfg config.ToolManifestConfig) *ToolManifest {
	if !cfg.Enabled {
		return nil
	}
	mappings := defaultToolManifestMappings()
	for k, v := range cfg.Mappings {
		key := strings.ToLower(strings.TrimSpace(k))
		switch {
		case v == nil:
			mappings[key] = nil
		case len(v) == 0:
			// Non-nil empty slice: "no tools". append(nil, v...) would collapse to nil ("all tools").
			mappings[key] = []string{}
		default:
			mappings[key] = append([]string(nil), v...)
		}
	}
	return &ToolManifest{
		cfg:        cfg,
		mappings:   mappings,
		classifier: NewTaskClassifier(cfg.MinConfidence),
	}
}

func defaultToolManifestMappings() map[string][]string {
	return map[string][]string{
		TaskTypeSummarize:  {},
		TaskTypeCodeGen:    {toolNameBash, toolNameRead, toolNameWrite},
		TaskTypeDocQA:      {toolNameRead},
		TaskTypeFullAgent:  nil, // nil = all tools
		TaskTypeWebResearch: nil,
	}
}

// Filter applies profile overrides, classification, and manifest mappings.
func (m *ToolManifest) Filter(
	tools []gateway.ToolDefinition,
	index map[string]string,
	task models.Task,
	profile models.AgentProfile,
) ([]gateway.ToolDefinition, map[string]string) {
	if m == nil {
		return tools, index
	}

	if allowed := normalizeToolNameSet(profile.AllowedTools); len(allowed) > 0 {
		slog.Debug("tool manifest: profile allowed_tools override",
			"task_id", task.ID,
			"tools", profile.AllowedTools,
		)
		return filterToolsByNames(tools, index, allowed)
	}

	taskType := strings.ToLower(strings.TrimSpace(profile.ToolManifestType))
	var classification TaskClassification
	if taskType != "" {
		classification = TaskClassification{Type: taskType, Confidence: 1}
	} else {
		classification = m.classifier.Classify(task)
		taskType = classification.Type
	}

	slog.Debug("tool manifest: classified task",
		"task_id", task.ID,
		"type", taskType,
		"confidence", classification.Confidence,
		"scores", classification.Scores,
	)

	names, ok := m.mappings[taskType]
	if !ok {
		return tools, index
	}
	if names == nil || manifestMeansAllTools(names) {
		return tools, index
	}

	allowed := normalizeToolNameSet(names)
	return filterToolsByNames(tools, index, allowed)
}

func manifestMeansAllTools(names []string) bool {
	if len(names) == 0 {
		return false
	}
	for _, n := range names {
		if strings.TrimSpace(n) == toolManifestWildcard {
			return true
		}
	}
	return false
}

func normalizeToolNameSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		key := strings.ToLower(strings.TrimSpace(v))
		if key != "" {
			out[key] = true
		}
	}
	return out
}

func filterToolsByNames(
	tools []gateway.ToolDefinition,
	index map[string]string,
	allowed map[string]bool,
) ([]gateway.ToolDefinition, map[string]string) {
	if len(allowed) == 0 {
		return nil, nil
	}
	var filtered []gateway.ToolDefinition
	for _, tool := range tools {
		if allowed[strings.ToLower(tool.Name)] {
			filtered = append(filtered, tool)
		}
	}
	if len(filtered) == 0 {
		return filtered, nil
	}
	if index == nil {
		return filtered, nil
	}
	newIndex := make(map[string]string, len(filtered))
	for _, tool := range filtered {
		if adapter, ok := index[tool.Name]; ok {
			newIndex[tool.Name] = adapter
		}
	}
	return filtered, newIndex
}
