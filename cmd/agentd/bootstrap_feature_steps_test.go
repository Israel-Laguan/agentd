package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// mockOpenAISuccessBody is a minimal valid OpenAI chat-completion response that
// satisfies the provider JSON parser and causes WarmupLLM to return nil.
const mockOpenAISuccessBody = `{"id":"chatcmpl-test","object":"chat.completion","model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"Hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`

// envNotSet is a sentinel stored in savedEnvs to indicate that the variable
// was not present in the environment before the scenario mutated it.
const envNotSet = "\x00unset"

// bootstrapScenario holds per-scenario state for the bootstrap feature tests.
type bootstrapScenario struct {
	homeDir    string
	lastErr    error
	lastOutput bytes.Buffer
	// gateway/api config assembled across Given/And steps
	gwOrder   string // provider name in gateway.order (default: "openai")
	openAIURL string // base_url override for mock LLM scenarios
	apiAddr   string // api.address override for port-conflict scenario
	// test resources
	mockSrv   *httptest.Server
	boundPort net.Listener
	savedEnvs map[string]string // key → original value (or envNotSet)
	tempFiles []string
}

func (s *bootstrapScenario) saveEnv(key string) {
	if _, exists := s.savedEnvs[key]; !exists {
		v, ok := os.LookupEnv(key)
		if ok {
			s.savedEnvs[key] = v
		} else {
			s.savedEnvs[key] = envNotSet
		}
	}
}

func registerBootstrapSteps(sc *godog.ScenarioContext) {
	s := &bootstrapScenario{}

	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*s = bootstrapScenario{savedEnvs: map[string]string{}}
		return ctx, nil
	})

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if s.boundPort != nil {
			_ = s.boundPort.Close()
		}
		if s.mockSrv != nil {
			s.mockSrv.Close()
		}
		for k, v := range s.savedEnvs {
			if v == envNotSet {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
		for _, f := range s.tempFiles {
			_ = os.Remove(f)
		}
		if s.homeDir != "" {
			_ = os.Chmod(s.homeDir, 0o755)
			_ = os.RemoveAll(s.homeDir)
		}
		return ctx, nil
	})

	// Given / And steps
	sc.Step(`^a fresh agentd home directory$`, s.freshHome)
	sc.Step(`^init has been run$`, s.initHasBeenRun)
	sc.Step(`^no LLM provider API keys are configured$`, s.noProviderKeys)
	sc.Step(`^an OpenAI API key is configured$`, s.openAIKeySet)
	sc.Step(`^the home directory is read-only$`, s.makeHomeDirReadOnly)
	sc.Step(`^an OpenAI provider pointing to a mock server that returns 500 is configured$`, s.mockServerFails)
	sc.Step(`^an OpenAI provider pointing to a mock server that returns 200 is configured$`, s.mockServerSucceeds)
	sc.Step(`^the configured API port is already bound$`, s.bindAPIPort)
	// When steps
	sc.Step(`^the init command is run$`, s.runInit)
	sc.Step(`^the init command is run again$`, s.runInit)
	sc.Step(`^the init command is run with the verbose flag$`, s.runVerboseInit)
	sc.Step(`^the start command is attempted$`, s.runStart)
	// Then steps
	sc.Step(`^the home directory should exist$`, s.homeDirExists)
	sc.Step(`^the database file should exist$`, s.dbFileExists)
	sc.Step(`^the database should use WAL journal mode$`, s.dbWALMode)
	sc.Step(`^the crontab file should exist$`, s.cronFileExists)
	sc.Step(`^the default agent profiles should be seeded$`, s.profilesSeeded)
	sc.Step(`^the output should contain "([^"]*)"$`, s.outputContains)
	sc.Step(`^the start command should have failed$`, s.startFailed)
	sc.Step(`^the error should describe an LLM provider configuration problem$`, s.errorDescribesLLMConfig)
	sc.Step(`^the error should describe a write permissions problem$`, s.errorDescribesWritePermissions)
	sc.Step(`^the error should describe an LLM warmup problem$`, s.errorDescribesWarmup)
	sc.Step(`^the error should describe an address binding problem$`, s.errorDescribesAddressConflict)
}

// --- Given / And ---

func (s *bootstrapScenario) freshHome(_ context.Context) error {
	s.homeDir = filepath.Join(os.TempDir(), fmt.Sprintf("agentd-boot-%d", time.Now().UnixNano()))
	return os.MkdirAll(s.homeDir, 0o755)
}

func (s *bootstrapScenario) initHasBeenRun(ctx context.Context) error {
	if err := s.runInit(ctx); err != nil {
		return err
	}
	if s.lastErr != nil {
		return fmt.Errorf("init failed during test setup: %w", s.lastErr)
	}
	return nil
}

func (s *bootstrapScenario) noProviderKeys(_ context.Context) error {
	// Clear all API key env vars that loadGatewayConfig reads as direct fallbacks.
	for _, k := range []string{
		"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY",
		"AGENTD_GATEWAY_OPENAI_API_KEY",
	} {
		s.saveEnv(k)
		_ = os.Setenv(k, "")
	}
	s.gwOrder = "openai"
	return nil
}

func (s *bootstrapScenario) openAIKeySet(_ context.Context) error {
	s.saveEnv("AGENTD_GATEWAY_OPENAI_API_KEY")
	_ = os.Setenv("AGENTD_GATEWAY_OPENAI_API_KEY", "test-key")
	s.gwOrder = "openai"
	return nil
}

func (s *bootstrapScenario) makeHomeDirReadOnly(_ context.Context) error {
	// All subdirs were already created by init; os.MkdirAll on them is a no-op.
	// The writability probe (os.CreateTemp) will fail with EACCES.
	return os.Chmod(s.homeDir, 0o555)
}

func (s *bootstrapScenario) mockServerFails(_ context.Context) error {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	s.mockSrv = srv
	s.gwOrder = "openai"
	s.openAIURL = srv.URL + "/v1"
	s.saveEnv("AGENTD_GATEWAY_OPENAI_API_KEY")
	_ = os.Setenv("AGENTD_GATEWAY_OPENAI_API_KEY", "test-key")
	return nil
}

func (s *bootstrapScenario) mockServerSucceeds(_ context.Context) error {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockOpenAISuccessBody))
	}))
	s.mockSrv = srv
	s.gwOrder = "openai"
	s.openAIURL = srv.URL + "/v1"
	s.saveEnv("AGENTD_GATEWAY_OPENAI_API_KEY")
	_ = os.Setenv("AGENTD_GATEWAY_OPENAI_API_KEY", "test-key")
	return nil
}

func (s *bootstrapScenario) bindAPIPort(_ context.Context) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("pre-bind port: %w", err)
	}
	s.boundPort = ln
	s.apiAddr = ln.Addr().String()
	return nil
}

// --- When ---

func (s *bootstrapScenario) runInit(ctx context.Context) error {
	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", s.homeDir, "init"})
	s.lastOutput.Reset()
	cmd.SetOut(&s.lastOutput)
	cmd.SetErr(&s.lastOutput)
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	s.lastErr = cmd.ExecuteContext(runCtx)
	return nil
}

func (s *bootstrapScenario) runVerboseInit(ctx context.Context) error {
	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", s.homeDir, "--verbose", "init"})
	s.lastOutput.Reset()
	cmd.SetOut(&s.lastOutput)
	cmd.SetErr(&s.lastOutput)
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	s.lastErr = cmd.ExecuteContext(runCtx)
	return nil
}

func (s *bootstrapScenario) runStart(ctx context.Context) error {
	configPath, err := s.buildStartConfig()
	if err != nil {
		return err
	}
	s.tempFiles = append(s.tempFiles, configPath)

	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", s.homeDir, "--config", configPath, "start"})
	s.lastOutput.Reset()
	cmd.SetOut(&s.lastOutput)
	cmd.SetErr(&s.lastOutput)
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	s.lastErr = cmd.ExecuteContext(runCtx)
	return nil
}

// buildStartConfig writes a minimal YAML config for the start command.
// gateway.order is always set to avoid slow health-check probes on providers
// that are not under test (Ollama, llamacpp, etc.).
func (s *bootstrapScenario) buildStartConfig() (string, error) {
	order := s.gwOrder
	if order == "" {
		order = "openai"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "gateway:\n  order:\n  - %s\n", order)
	if s.openAIURL != "" {
		fmt.Fprintf(&sb, "  openai:\n    base_url: %q\n", s.openAIURL)
	}
	if s.apiAddr != "" {
		fmt.Fprintf(&sb, "api:\n  address: %q\n", s.apiAddr)
	}
	configPath := filepath.Join(os.TempDir(), fmt.Sprintf("agentd-bscfg-%d.yaml", time.Now().UnixNano()))
	return configPath, os.WriteFile(configPath, []byte(sb.String()), 0o644)
}
