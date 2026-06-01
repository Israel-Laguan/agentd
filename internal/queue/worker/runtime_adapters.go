package worker

import (
	"strings"

	agentruntime "agentd/internal/agent/runtime"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// applyModelRouting selects provider/model from complexity routing when enabled.
func (w *Worker) applyModelRouting(
	task models.Task,
	profile models.AgentProfile,
	messages []gateway.PromptMessage,
	tools []gateway.ToolDefinition,
) models.AgentProfile {
	if w.modelRouter == nil {
		return profile
	}
	contextTokens := agentruntime.EstimateContextTokens(messages, tools)
	provider, model, ok := w.modelRouter.Route(task, contextTokens)
	if !ok {
		return profile
	}
	profile.Provider = provider
	profile.Model = model
	return profile
}

func (w *Worker) shouldUseCodePromptTemplate(task models.Task, profile models.AgentProfile) bool {
	if strings.EqualFold(strings.TrimSpace(profile.ToolManifestType), agenttools.TaskTypeCodeGen) {
		return true
	}
	if w.toolManifest == nil {
		return false
	}
	classification := w.toolManifest.ClassifyTask(task)
	return classification.Type == agenttools.TaskTypeCodeGen && classification.Confidence >= w.toolManifest.MinConfidence()
}

func (w *Worker) promptTemplateForTask(task models.Task, profile models.AgentProfile) (name string, slots map[string]string, ok bool) {
	if w.promptLibrary == nil {
		return "", nil, false
	}
	if w.shouldUseCodePromptTemplate(task, profile) {
		return agentruntime.TemplateCodePromptBuilder, agentruntime.BuildCodeGenSlots(task), true
	}
	return "", nil, false
}

type delegateArgs struct {
	Subagent string `json:"subagent"`
	Task     string `json:"task"`
}

type delegateParallelArgs struct {
	Tasks []delegateArgs `json:"tasks"`
}
