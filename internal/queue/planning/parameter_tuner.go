package planning

import (
	"fmt"
	"strings"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

const (
	HealingStepLowerTemperature = "lower_temperature"
	HealingStepIncreaseContext  = "increase_context"
	HealingStepCompressContext  = "compress_context"
	HealingStepUpgradeModel     = "upgrade_model"
	HealingStepSplitTask        = "split_task"
	HealingStepHumanHandoff     = "human_handoff"

	defaultContextTokens = 4096
)

type HealingActionType string

const (
	HealingActionTune  HealingActionType = "tune"
	HealingActionSplit HealingActionType = "split"
	HealingActionHuman HealingActionType = "human"
)

type TuneOverrides struct {
	Temperature *float64
	MaxTokens   *int
	Model       string
	Provider    string
	Compress    bool
}

type HealingAction struct {
	Type      HealingActionType
	Overrides TuneOverrides
	StepName  string
	Reason    string
}

type ParameterTuner struct {
	steps             []string
	maxAdjustments    int
	upgradeModel      string
	upgradeProvider   string
	contextMultiplier float64
}

func NewParameterTuner(cfg config.HealingConfig) *ParameterTuner {
	if !cfg.Enabled {
		return nil
	}
	steps := cfg.Steps
	if len(steps) == 0 {
		steps = presetSteps(cfg.Strategy)
	}
	if len(steps) == 0 {
		return nil
	}
	maxAdjustments := cfg.MaxAdjustments
	if maxAdjustments <= 0 || maxAdjustments > len(steps) {
		maxAdjustments = len(steps)
	}
	multiplier := cfg.ContextMultiplier
	if multiplier <= 1 {
		multiplier = 2
	}
	return &ParameterTuner{
		steps:             append([]string(nil), steps...),
		maxAdjustments:    maxAdjustments,
		upgradeModel:      cfg.UpgradeModel,
		upgradeProvider:   cfg.UpgradeProvider,
		contextMultiplier: multiplier,
	}
}

func presetSteps(strategy string) []string {
	switch strategy {
	case config.HealingStrategyMinimizeVariables:
		return []string{
			HealingStepLowerTemperature,
			HealingStepCompressContext,
			HealingStepUpgradeModel,
			HealingStepIncreaseContext,
			HealingStepSplitTask,
			HealingStepHumanHandoff,
		}
	default:
		return []string{
			HealingStepLowerTemperature,
			HealingStepIncreaseContext,
			HealingStepCompressContext,
			HealingStepUpgradeModel,
			HealingStepSplitTask,
			HealingStepHumanHandoff,
		}
	}
}

func (t *ParameterTuner) ForAttempt(retryCount int, profile models.AgentProfile) HealingAction {
	if t == nil || retryCount <= 0 {
		return HealingAction{}
	}
	index := retryCount - 1
	if index >= t.maxAdjustments || index >= len(t.steps) {
		return t.humanAction("healing budget exhausted")
	}
	step := strings.TrimSpace(t.steps[index])
	switch step {
	case HealingStepLowerTemperature:
		zero := 0.0
		return HealingAction{
			Type:      HealingActionTune,
			StepName:  step,
			Reason:    "reduce randomness after repeated failure",
			Overrides: TuneOverrides{Temperature: &zero},
		}
	case HealingStepIncreaseContext:
		maxTokens := defaultContextTokens
		if next := int(float64(maxTokens) * t.contextMultiplier); next > maxTokens {
			maxTokens = next
		}
		return HealingAction{
			Type:      HealingActionTune,
			StepName:  step,
			Reason:    "allow a larger response/context budget",
			Overrides: TuneOverrides{MaxTokens: &maxTokens},
		}
	case HealingStepCompressContext:
		return HealingAction{
			Type:      HealingActionTune,
			StepName:  step,
			Reason:    "reduce prompt variables and focus on the failing task",
			Overrides: TuneOverrides{Compress: true},
		}
	case HealingStepUpgradeModel:
		model := t.upgradeModel
		if model == "" {
			model = profile.Model
		}
		return HealingAction{
			Type:      HealingActionTune,
			StepName:  step,
			Reason:    "try a higher-capability model/provider",
			Overrides: TuneOverrides{Model: model, Provider: t.upgradeProvider},
		}
	case HealingStepSplitTask:
		return HealingAction{Type: HealingActionSplit, StepName: step, Reason: "split work after repeated failures"}
	case HealingStepHumanHandoff:
		return t.humanAction("automatic healing reached human handoff step")
	default:
		return t.humanAction(fmt.Sprintf("unknown healing step %q", step))
	}
}

func (t *ParameterTuner) Apply(req gateway.AIRequest, action HealingAction) gateway.AIRequest {
	if action.Overrides.Temperature != nil {
		req.Temperature = *action.Overrides.Temperature
	}
	if action.Overrides.MaxTokens != nil {
		req.MaxTokens = *action.Overrides.MaxTokens
	}
	if action.Overrides.Model != "" {
		req.Model = action.Overrides.Model
	}
	if action.Overrides.Provider != "" {
		req.Provider = action.Overrides.Provider
	}
	if action.Overrides.Compress {
		req.Messages = compactWorkerMessages(req.Messages)
	}
	return req
}

func (t *ParameterTuner) humanAction(reason string) HealingAction {
	return HealingAction{Type: HealingActionHuman, StepName: HealingStepHumanHandoff, Reason: reason}
}

func compactWorkerMessages(messages []gateway.PromptMessage) []gateway.PromptMessage {
	const maxContent = 2000
	out := append([]gateway.PromptMessage(nil), messages...)
	for i := range out {
		if out[i].Role != "user" || len(out[i].Content) <= maxContent {
			continue
		}
		head := maxContent / 2
		tail := maxContent - head
		out[i].Content = out[i].Content[:head] + "\n...[compressed]...\n" + out[i].Content[len(out[i].Content)-tail:]
	}
	return out
}
