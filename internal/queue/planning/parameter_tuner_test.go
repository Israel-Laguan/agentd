package planning

import (
	"strings"
	"testing"
	"unicode/utf8"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func TestNewParameterTuner_DisabledReturnsNil(t *testing.T) {
	if got := NewParameterTuner(config.HealingConfig{Enabled: false}); got != nil {
		t.Fatalf("NewParameterTuner(disabled) = %v, want nil", got)
	}
}

func TestNewParameterTuner_PresetStepsDefaultAndMinimizeVariables(t *testing.T) {
	defaultTuner := NewParameterTuner(config.HealingConfig{Enabled: true})
	if defaultTuner == nil || len(defaultTuner.steps) == 0 {
		t.Fatal("expected default preset steps")
	}
	if defaultTuner.steps[0] != HealingStepLowerTemperature {
		t.Fatalf("default first step = %q", defaultTuner.steps[0])
	}

	minTuner := NewParameterTuner(config.HealingConfig{
		Enabled:  true,
		Strategy: config.HealingStrategyMinimizeVariables,
	})
	if minTuner == nil {
		t.Fatal("expected minimize_variables preset")
	}
	if minTuner.steps[1] != HealingStepCompressContext {
		t.Fatalf("minimize_variables second step = %q, want compress_context", minTuner.steps[1])
	}
}

func TestNewParameterTuner_MaxAdjustmentsClamped(t *testing.T) {
	tuner := NewParameterTuner(config.HealingConfig{
		Enabled:        true,
		Steps:          []string{HealingStepLowerTemperature, HealingStepSplitTask},
		MaxAdjustments: 10,
	})
	if tuner.maxAdjustments != 2 {
		t.Fatalf("maxAdjustments = %d, want 2 (clamped to len(steps))", tuner.maxAdjustments)
	}

	zero := NewParameterTuner(config.HealingConfig{
		Enabled: true,
		Steps:   []string{HealingStepLowerTemperature},
	})
	if zero.maxAdjustments != 1 {
		t.Fatalf("maxAdjustments = %d, want 1 when unset", zero.maxAdjustments)
	}
}

func TestNewParameterTuner_ContextMultiplierDefault(t *testing.T) {
	tuner := NewParameterTuner(config.HealingConfig{
		Enabled:           true,
		Steps:             []string{HealingStepIncreaseContext},
		ContextMultiplier: 0,
	})
	if tuner.contextMultiplier != 2 {
		t.Fatalf("contextMultiplier = %v, want 2", tuner.contextMultiplier)
	}
}

func TestForAttempt_LowerTemperature(t *testing.T) {
	tuner := NewParameterTuner(config.HealingConfig{
		Enabled: true,
		Steps:   []string{HealingStepLowerTemperature},
	})
	action := tuner.ForAttempt(1, models.AgentProfile{})
	if action.Type != HealingActionTune || action.StepName != HealingStepLowerTemperature {
		t.Fatalf("action = %+v", action)
	}
	if action.Overrides.Temperature == nil || *action.Overrides.Temperature != 0 {
		t.Fatalf("temperature override = %v", action.Overrides.Temperature)
	}
}

func TestForAttempt_IncreaseContext(t *testing.T) {
	tuner := NewParameterTuner(config.HealingConfig{
		Enabled:           true,
		Steps:             []string{HealingStepIncreaseContext},
		ContextMultiplier: 3,
	})
	action := tuner.ForAttempt(1, models.AgentProfile{})
	if action.Overrides.MaxTokens == nil || *action.Overrides.MaxTokens != defaultContextTokens*3 {
		t.Fatalf("max tokens = %v, want %d", action.Overrides.MaxTokens, defaultContextTokens*3)
	}
}

func TestForAttempt_CompressContext(t *testing.T) {
	tuner := NewParameterTuner(config.HealingConfig{
		Enabled: true,
		Steps:   []string{HealingStepCompressContext},
	})
	action := tuner.ForAttempt(1, models.AgentProfile{})
	if !action.Overrides.Compress {
		t.Fatal("expected Compress override")
	}
}

func TestForAttempt_UpgradeModelFallsBackToProfile(t *testing.T) {
	tuner := NewParameterTuner(config.HealingConfig{
		Enabled: true,
		Steps:   []string{HealingStepUpgradeModel},
	})
	action := tuner.ForAttempt(1, models.AgentProfile{Model: "profile-model"})
	if action.Overrides.Model != "profile-model" {
		t.Fatalf("model = %q, want profile-model", action.Overrides.Model)
	}

	withUpgrade := NewParameterTuner(config.HealingConfig{
		Enabled:      true,
		Steps:        []string{HealingStepUpgradeModel},
		UpgradeModel: "strong",
	})
	action = withUpgrade.ForAttempt(1, models.AgentProfile{Model: "profile-model"})
	if action.Overrides.Model != "strong" {
		t.Fatalf("model = %q, want strong", action.Overrides.Model)
	}
}

func TestForAttempt_SplitAndHumanHandoff(t *testing.T) {
	splitTuner := NewParameterTuner(config.HealingConfig{
		Enabled: true,
		Steps:   []string{HealingStepSplitTask},
	})
	if action := splitTuner.ForAttempt(1, models.AgentProfile{}); action.Type != HealingActionSplit {
		t.Fatalf("split action = %+v", action)
	}

	humanTuner := NewParameterTuner(config.HealingConfig{
		Enabled: true,
		Steps:   []string{HealingStepHumanHandoff},
	})
	if action := humanTuner.ForAttempt(1, models.AgentProfile{}); action.Type != HealingActionHuman {
		t.Fatalf("human action = %+v", action)
	}
}

func TestForAttempt_ExhaustedBudget(t *testing.T) {
	tuner := NewParameterTuner(config.HealingConfig{
		Enabled:        true,
		Steps:          []string{HealingStepLowerTemperature},
		MaxAdjustments: 1,
	})
	action := tuner.ForAttempt(2, models.AgentProfile{})
	if action.Type != HealingActionHuman {
		t.Fatalf("action = %+v, want human handoff", action)
	}
}

func TestForAttempt_UnknownStep(t *testing.T) {
	tuner := NewParameterTuner(config.HealingConfig{
		Enabled: true,
		Steps:   []string{"bogus_step"},
	})
	action := tuner.ForAttempt(1, models.AgentProfile{})
	if action.Type != HealingActionHuman || !strings.Contains(action.Reason, "unknown healing step") {
		t.Fatalf("action = %+v", action)
	}
}

func TestForAttempt_ZeroRetryReturnsEmpty(t *testing.T) {
	tuner := NewParameterTuner(config.HealingConfig{Enabled: true, Steps: []string{HealingStepLowerTemperature}})
	if action := tuner.ForAttempt(0, models.AgentProfile{}); action.Type != "" {
		t.Fatalf("action = %+v, want empty", action)
	}
}

func TestApply_AllOverrideFields(t *testing.T) {
	tuner := NewParameterTuner(config.HealingConfig{Enabled: true})
	temp := 0.1
	tokens := 8192
	req := gateway.AIRequest{
		Temperature: 0.9,
		MaxTokens:   100,
		Model:       "old",
		Provider:    "old-prov",
		Messages:    []gateway.PromptMessage{{Role: "user", Content: "hi"}},
	}
	out := tuner.Apply(req, HealingAction{
		Overrides: TuneOverrides{
			Temperature: &temp,
			MaxTokens:   &tokens,
			Model:       "new",
			Provider:    "new-prov",
			Compress:    true,
		},
	})
	if out.Temperature != 0.1 || out.MaxTokens != 8192 || out.Model != "new" || out.Provider != "new-prov" {
		t.Fatalf("Apply overrides = %+v", out)
	}
}

func TestCompactWorkerMessages_TruncatesLongUserContent(t *testing.T) {
	long := strings.Repeat("x", 3000)
	msgs := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: long},
	}
	out := compactWorkerMessages(msgs)
	if utf8.RuneCountInString(out[1].Content) > 2100 {
		t.Fatalf("content still too long: %d runes", utf8.RuneCountInString(out[1].Content))
	}
	if !strings.Contains(out[1].Content, "...[compressed]") {
		t.Fatalf("content = %q, want compression marker", out[1].Content)
	}
	if out[0].Content != "sys" {
		t.Fatal("system message should be unchanged")
	}
}
