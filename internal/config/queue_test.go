package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestQueueDefaults(t *testing.T) {
	v := viper.New()
	setQueueDefaults(v)
	cfg := loadQueueConfig(v)

	if cfg.TaskDeadline != DefaultTaskDeadline {
		t.Fatalf("TaskDeadline = %v, want %v", cfg.TaskDeadline, DefaultTaskDeadline)
	}
	if cfg.QueuedReconcileAfter != DefaultQueuedReconcileAfter {
		t.Fatalf("QueuedReconcileAfter = %v, want %v", cfg.QueuedReconcileAfter, DefaultQueuedReconcileAfter)
	}
}

func TestQueueConfigOverride_QueuedReconcileAfter(t *testing.T) {
	v := viper.New()
	setQueueDefaults(v)
	v.Set("queue.task_deadline", "1m")
	v.Set("queue.queued_reconcile_after", "15m")
	cfg := loadQueueConfig(v)

	if cfg.TaskDeadline != time.Minute {
		t.Fatalf("TaskDeadline = %v, want 1m", cfg.TaskDeadline)
	}
	if cfg.QueuedReconcileAfter != 15*time.Minute {
		t.Fatalf("QueuedReconcileAfter = %v, want 15m", cfg.QueuedReconcileAfter)
	}
}

func TestQueueConfigOverride_LegacyHandoffTimeout(t *testing.T) {
	v := viper.New()
	setQueueDefaults(v)
	v.Set("queue.hitl.legacy_handoff_timeout", "48h")
	cfg := loadQueueConfig(v)

	if cfg.HITL.LegacyHandoffTimeout != 48*time.Hour {
		t.Fatalf("LegacyHandoffTimeout = %v, want 48h", cfg.HITL.LegacyHandoffTimeout)
	}
}

func TestQueueConfig_Defaults(t *testing.T) {
	cfg := QueueConfig{
		TaskDeadline:    DefaultTaskDeadline,
		PollMaxInterval: DefaultPollMaxInterval,
	}
	if cfg.TaskDeadline != 10*time.Minute {
		t.Errorf("TaskDeadline = %v, want 10m", cfg.TaskDeadline)
	}
	if cfg.PollMaxInterval != 10*time.Second {
		t.Errorf("PollMaxInterval = %v, want 10s", cfg.PollMaxInterval)
	}
}

func TestQueueConfig_Custom(t *testing.T) {
	cfg := QueueConfig{
		TaskDeadline:    30 * time.Minute,
		PollMaxInterval: 5 * time.Second,
	}
	if cfg.TaskDeadline != 30*time.Minute {
		t.Errorf("TaskDeadline = %v, want 30m", cfg.TaskDeadline)
	}
	if cfg.PollMaxInterval != 5*time.Second {
		t.Errorf("PollMaxInterval = %v, want 5s", cfg.PollMaxInterval)
	}
}

type agenticCharacterBudgetCompatCase struct {
	name     string
	set      func(*viper.Viper)
	want     int
	wantWarn bool
}

func agenticCharacterBudgetCompatCases() []agenticCharacterBudgetCompatCase {
	return []agenticCharacterBudgetCompatCase{
		{
			name: "default",
			set:  func(*viper.Viper) {},
			want: DefaultAgenticCharacterBudget,
		},
		{
			name: "new key explicit",
			set: func(v *viper.Viper) {
				v.Set("queue.agentic_character_budget", 5000)
			},
			want: 5000,
		},
		{
			name: "new key explicit zero ignores legacy",
			set: func(v *viper.Viper) {
				v.Set("queue.agentic_character_budget", 0)
				v.Set("queue.agentic_truncation_threshold", 40)
			},
			want:     0,
			wantWarn: true,
		},
		{
			name: "legacy only",
			set: func(v *viper.Viper) {
				v.Set("queue.agentic_truncation_threshold", 40)
			},
			want:     DefaultAgenticCharacterBudget,
			wantWarn: true,
		},
		{
			name: "both set prefers new",
			set: func(v *viper.Viper) {
				v.Set("queue.agentic_character_budget", 100)
				v.Set("queue.agentic_truncation_threshold", 40)
			},
			want:     100,
			wantWarn: true,
		},
	}
}

func runAgenticCharacterBudgetCompatCase(t *testing.T, tc agenticCharacterBudgetCompatCase) {
	t.Helper()

	v := viper.New()
	setQueueDefaults(v)
	tc.set(v)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cfg := loadQueueConfig(v)
	if cfg.AgenticCharacterBudget != tc.want {
		t.Fatalf("AgenticCharacterBudget = %d, want %d", cfg.AgenticCharacterBudget, tc.want)
	}
	hasWarn := strings.Contains(buf.String(), "deprecated config key") ||
		strings.Contains(buf.String(), "both config keys set")
	if hasWarn != tc.wantWarn {
		t.Fatalf("warning logged = %v, want %v; log: %q", hasWarn, tc.wantWarn, buf.String())
	}
}

func TestQueueConfig_AgenticCharacterBudgetCompat(t *testing.T) {
	for _, tc := range agenticCharacterBudgetCompatCases() {
		t.Run(tc.name, func(t *testing.T) {
			runAgenticCharacterBudgetCompatCase(t, tc)
		})
	}
}

func TestLoadAgenticCharacterBudget_LegacyInheritsGateway(t *testing.T) {
	t.Parallel()

	v := viper.New()
	setQueueDefaults(v)
	v.Set(queueKeyAgenticTruncationThreshold, 40)

	if got := loadAgenticCharacterBudget(v); got != 0 {
		t.Fatalf("loadAgenticCharacterBudget() = %d, want 0 (legacy message count ignored)", got)
	}
	if got := EffectiveAgenticCharacterBudget(loadAgenticCharacterBudget(v), 12000); got != 12000 {
		t.Fatalf("EffectiveAgenticCharacterBudget() = %d, want 12000 (inherit gateway)", got)
	}
}

func TestEffectiveAgenticCharacterBudget(t *testing.T) {
	t.Parallel()

	if got := EffectiveAgenticCharacterBudget(5000, 12000); got != 5000 {
		t.Fatalf("explicit budget = %d, want 5000", got)
	}
	if got := EffectiveAgenticCharacterBudget(0, 12000); got != 12000 {
		t.Fatalf("inherit gateway = %d, want 12000", got)
	}
	if got := EffectiveAgenticCharacterBudget(0, 0); got != 0 {
		t.Fatalf("both zero = %d, want 0", got)
	}
}

func TestDefaultQueueValues(t *testing.T) {
	if DefaultTaskDeadline != 10*time.Minute {
		t.Errorf("DefaultTaskDeadline = %v, want 10m", DefaultTaskDeadline)
	}
	if DefaultQueuedReconcileAfter != 10*time.Minute {
		t.Errorf("DefaultQueuedReconcileAfter = %v, want 10m", DefaultQueuedReconcileAfter)
	}
	if DefaultPollMaxInterval != 10*time.Second {
		t.Errorf("DefaultPollMaxInterval = %v, want 10s", DefaultPollMaxInterval)
	}
}
