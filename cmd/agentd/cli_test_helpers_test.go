package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func runCLI(t *testing.T, home string, args ...string) string {
	t.Helper()
	t.Setenv("AGENTD_GATEWAY_OPENAI_API_KEY", "test-key")

	full := append([]string{"--home", home}, args...)
	cmd := newRootCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(full)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("agentd %v error = %v", args, err)
	}
	return output.String()
}

func initHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), ".agentd")
	runCLI(t, home, "init")
	return home
}

func clearProviderKeys(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY",
		"AGENTD_GATEWAY_OPENAI_API_KEY", "AGENTD_GATEWAY_ANTHROPIC_API_KEY",
		"AGENTD_GATEWAY_GEMINI_API_KEY",
	} {
		t.Setenv(key, "")
	}
}

func writeStartTestConfig(t *testing.T) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "agentd.yaml")
	// Restrict gateway.order to openai so a local Ollama instance cannot satisfy
	// provider checks and cause start to block until the test context expires.
	if err := os.WriteFile(configPath, []byte("gateway:\n  order:\n    - openai\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}
