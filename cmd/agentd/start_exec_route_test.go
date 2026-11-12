package main

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/gateway/route"
	"agentd/internal/gateway/spec"
	"agentd/internal/kanban"
)

func TestStartupWarnsWithoutExecTurnACPRoute(t *testing.T) {
	dir := t.TempDir()
	store, err := kanban.OpenStore(filepath.Join(dir, "agentd.db"), filepath.Join(dir, "projects"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	cfg := config.Config{Gateway: config.GatewayConfig{Order: []string{"openai"}, OpenAI: gateway.ProviderConfig{APIKey: "test-key"}}}
	if err := seedAndValidateStartup(context.Background(), store, cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "WARN") || !strings.Contains(logs.String(), "exec.turn") {
		t.Fatalf("startup log = %q, want a warning naming missing exec.turn ACP route", logs.String())
	}
}

func TestStartupWarningOnlyForMissingExecTurnACPRoute(t *testing.T) {
	write := true
	for _, tc := range []struct {
		name  string
		table route.Table
		warn  bool
	}{
		{name: "unrelated ACP agent", table: route.Table{Agents: map[string]route.Agent{"coder": {Kind: route.KindACP}}}, warn: true},
		{name: "model route", table: route.Table{Agents: map[string]route.Agent{"model": {Kind: route.KindModel}}, Stages: map[spec.Purpose]route.StageRoute{spec.PurposeExecTurn: {Default: "model"}}}, warn: true},
		{name: "ACP default", table: route.Table{Agents: map[string]route.Agent{"coder": {Kind: route.KindACP}}, Stages: map[spec.Purpose]route.StageRoute{spec.PurposeExecTurn: {Default: "coder"}}}},
		{name: "ACP rule", table: route.Table{Agents: map[string]route.Agent{"coder": {Kind: route.KindACP}}, Stages: map[spec.Purpose]route.StageRoute{spec.PurposeExecTurn: {Rules: []route.Rule{{Match: route.Match{RequiresWorkspaceChange: &write}, Use: "coder"}}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			defer slog.SetDefault(previous)
			warnMissingExecutionRoute(tc.table)
			if got := strings.Contains(logs.String(), "exec.turn"); got != tc.warn {
				t.Fatalf("warning = %t, want %t: %s", got, tc.warn, logs.String())
			}
		})
	}
}
