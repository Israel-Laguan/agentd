package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadConfigBody writes body as the config file and loads it.
func loadConfigBody(t *testing.T, body string) (Config, error) {
	t.Helper()
	homeDir := filepath.Join(t.TempDir(), "agentd")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "agentd.yaml")
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return Load(LoadOptions{HomeOverride: homeDir, ConfigFile: configPath})
}

// A removed key is ignored by viper, so a user's old setting would stop
// working with no warning. Load must refuse it and name what replaces it.
func TestLoad_RemovedKeyFailsAndNamesReplacement(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "breakdown depth names split.max_depth",
			body: "queue:\n  legacy:\n    max_breakdown_depth: 3\n",
			want: []string{"queue.legacy.max_breakdown_depth", "queue.split.max_depth"},
		},
		{
			name: "subtask cap names split.max_subtasks",
			body: "queue:\n  legacy:\n    max_subtasks_per_breakdown: 4\n",
			want: []string{"queue.legacy.max_subtasks_per_breakdown", "queue.split.max_subtasks"},
		},
		{
			name: "legacy score has no replacement",
			body: "queue:\n  legacy:\n    preflight_score: 6\n",
			want: []string{"queue.legacy.preflight_score", "no replacement"},
		},
		{
			name: "reject score has no replacement",
			body: "queue:\n  legacy:\n    reject_score: 9\n",
			want: []string{"queue.legacy.reject_score", "no replacement"},
		},
		{
			name: "description length has no replacement",
			body: "queue:\n  legacy:\n    max_description_len: 2000\n",
			want: []string{"queue.legacy.max_description_len", "no replacement"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadConfigBody(t, tc.body)
			if err == nil {
				t.Fatal("Load() accepted a removed key, want an error")
			}
			for _, sub := range tc.want {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("error %q does not contain %q", err, sub)
				}
			}
			// The CLI picks its config hint from ErrConfigRead. Without the wrap a
			// removed key gets a generic failure message.
			if !errors.Is(err, ErrConfigRead) {
				t.Errorf("error %q does not wrap ErrConfigRead", err)
			}
		})
	}
}

// Env vars are config too: AGENTD_QUEUE_LEGACY_REJECT_SCORE is the same key.
func TestLoad_RemovedKeyFromEnvFails(t *testing.T) {
	t.Setenv("AGENTD_QUEUE_LEGACY_REJECT_SCORE", "9")
	_, err := loadConfigBody(t, "")
	if err == nil || !strings.Contains(err.Error(), "queue.legacy.reject_score") {
		t.Fatalf("Load() error = %v, want the removed env key named", err)
	}
}

// Viper treats an empty env var as unset, so an empty AGENTD_* setting is
// caught only by the process environment itself.
func TestLoad_EmptyRemovedEnvFails(t *testing.T) {
	t.Setenv("AGENTD_QUEUE_LEGACY_REJECT_SCORE", "")
	_, err := loadConfigBody(t, "")
	if err == nil || !strings.Contains(err.Error(), "queue.legacy.reject_score") {
		t.Fatalf("Load() error = %v, want the empty removed env key named", err)
	}
}

// applyDotEnvToViper forwards only keys viper already knows, and removed keys
// have no defaults, so a removed key in .env needs its own check.
func TestLoad_RemovedKeyFromDotEnvFails(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "agentd")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".env"), []byte("AGENTD_QUEUE_LEGACY_MAX_DESCRIPTION_LEN=2000\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	_, err := Load(LoadOptions{HomeOverride: homeDir})
	if err == nil || !strings.Contains(err.Error(), "queue.legacy.max_description_len") {
		t.Fatalf("Load() error = %v, want the removed .env key named", err)
	}
}

// Replacing a removed key with its successor loads cleanly.
func TestLoad_ReplacementKeyLoads(t *testing.T) {
	cfg, err := loadConfigBody(t, "queue:\n  split:\n    max_depth: 4\n")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Queue.Split.MaxDepth != 4 {
		t.Fatalf("Queue.Split.MaxDepth = %d, want 4", cfg.Queue.Split.MaxDepth)
	}
}
