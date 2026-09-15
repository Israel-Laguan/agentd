package main

import (
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
)

type initProfileHintCase struct {
	name    string
	gw      config.GatewayConfig
	want    []string
	notWant []string
}

var initProfileHintCases = []initProfileHintCase{
	{
		name: "no keys",
		gw:   config.GatewayConfig{Order: []string{"openai"}},
		want: []string{
			"gateway.order cascade",
			"no LLM API keys detected",
			"PATCH /api/v1/agents/<id>",
			"repeat init preserves",
			"--reset-profiles",
		},
		notWant: []string{"--skip-llm-warmup"},
	},
	{
		name: "gemini in order with key",
		gw: config.GatewayConfig{
			Order: []string{"gemini"},
			Gemini: gateway.ProviderConfig{
				APIKey: "test-key",
				Model:  "gemini-2.5-flash",
			},
		},
		want: []string{
			"detected LLM provider: gemini",
			"--skip-llm-warmup",
			"PATCH /api/v1/agents/<id>",
		},
	},
	{
		name: "openai key with gemini in order",
		gw: config.GatewayConfig{
			Order:  []string{"openai", "gemini"},
			OpenAI: gateway.ProviderConfig{APIKey: "sk-test"},
		},
		want: []string{
			"detected LLM provider: openai",
			"PATCH /api/v1/agents/<id>",
			"--skip-llm-warmup",
		},
	},
	{
		name: "openai only",
		gw: config.GatewayConfig{
			Order:  []string{"openai"},
			OpenAI: gateway.ProviderConfig{APIKey: "sk-test"},
		},
		want: []string{
			"detected LLM provider: openai",
		},
		notWant: []string{"--skip-llm-warmup"},
	},
}

func TestInitProfileHint(t *testing.T) {
	t.Parallel()
	for _, tc := range initProfileHintCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := initProfileHint(tc.gw)
			assertContainsAll(t, got, tc.want)
			assertContainsNone(t, got, tc.notWant)
		})
	}
}

func assertContainsAll(t *testing.T, got string, want []string) {
	t.Helper()
	for _, s := range want {
		if !strings.Contains(got, s) {
			t.Errorf("initProfileHint missing %q\n%s", s, got)
		}
	}
}

func assertContainsNone(t *testing.T, got string, notWant []string) {
	t.Helper()
	for _, s := range notWant {
		if strings.Contains(got, s) {
			t.Errorf("initProfileHint should not contain %q\n%s", s, got)
		}
	}
}
