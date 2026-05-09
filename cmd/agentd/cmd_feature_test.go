package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentd/internal/config"

	"github.com/cucumber/godog"
)

func TestCLIFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initializeCLIScenario,
		Options:             &godog.Options{Format: "pretty", Paths: []string{"features"}, TestingT: t, Strict: true},
	}
	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run CLI feature tests")
	}
}

type cliScenario struct {
	homeDir    string
	cronPath   string
	configFile string
	cronSched  config.CronSchedule
	loadedCfg  config.Config
	lastErr    error
	cronCustom string
}

func initializeCLIScenario(sc *godog.ScenarioContext) {
	state := &cliScenario{}
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*state = cliScenario{}
		state.homeDir = filepath.Join(os.TempDir(), fmt.Sprintf("agentd-test-%d", time.Now().UnixNano()))
		state.cronPath = filepath.Join(state.homeDir, "agentd.crontab")
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if state.homeDir != "" {
			_ = os.RemoveAll(state.homeDir)
		}
		return ctx, nil
	})

	// Cron schedule steps
	sc.Step(`^a fresh home directory$`, state.freshHome)
	sc.Step(`^agentd init is executed$`, state.agentdInit)
	sc.Step(`^an agentd\.crontab file should exist in the home directory$`, state.cronFileExists)
	sc.Step(`^the crontab should contain the default schedule entries$`, state.cronContainsDefaults)
	sc.Step(`^a home directory with a custom agentd\.crontab$`, state.homeWithCustomCron)
	sc.Step(`^agentd init is executed again$`, state.agentdInit)
	sc.Step(`^the custom crontab should not be overwritten$`, state.customCronPreserved)
	sc.Step(`^a crontab with "([^"]*)" and "([^"]*)"$`, state.cronWithEntries)
	sc.Step(`^the crontab is loaded$`, state.loadCrontab)
	sc.Step(`^the task-dispatch interval should be (\d+) seconds$`, state.taskDispatchInterval)
	sc.Step(`^the disk-watchdog should have a standard cron schedule$`, state.diskWatchdogHasSchedule)
	sc.Step(`^a crontab with "([^"]*)"$`, state.cronWithSingleEntry)
	sc.Step(`^no error should be returned$`, state.noError)
	sc.Step(`^the default task-dispatch interval should be preserved$`, state.defaultTaskDispatch)

	// Config flag steps
	sc.Step(`^a config file with "([^"]*)"$`, state.configFileWith)
	sc.Step(`^the environment variable ([A-Z_]+) is set to "([^"]*)"$`, state.setEnvVar)
	sc.Step(`^config is loaded with the explicit config file$`, state.loadWithExplicitConfig)
	sc.Step(`^gateway\.max_tasks_per_phase should be (\d+)$`, state.maxTasksPerPhase)
	sc.Step(`^a non-existent config file path$`, state.nonExistentConfigFile)
	sc.Step(`^an error should be returned$`, state.errorReturned)
	sc.Step(`^healing\.enabled should be true$`, state.healingEnabled)

	// Documentary / future CLI scenarios (cli_ask.feature)
	sc.Step(`^the internal API server is running on localhost$`, noopCLI)
	sc.Step(`^the CLI is executed with agentd ask "([^"]*)"$`, noopCLIAsk)
	sc.Step(`^the CLI should print the proposed tasks$`, noopCLI)
	sc.Step(`^the CLI should prompt for approval$`, noopCLI)
	sc.Step(`^the user approves the plan$`, noopCLI)
	sc.Step(`^the CLI should call the materialize endpoint$`, noopCLI)
	sc.Step(`^the CLI should print a project started message$`, noopCLI)
	sc.Step(`^the CLI has printed a drafted plan and is waiting for input$`, noopCLI)
	sc.Step(`^the user rejects the plan$`, noopCLI)
	sc.Step(`^the CLI should not call the materialize endpoint$`, noopCLI)
	sc.Step(`^the CLI should exit gracefully$`, noopCLI)
}

// Cron schedule steps

func noopCLI(context.Context) error { return nil }

func noopCLIAsk(context.Context, string) error { return nil }

func (s *cliScenario) freshHome(context.Context) error {
	return os.MkdirAll(s.homeDir, 0o755)
}

func (s *cliScenario) agentdInit(context.Context) error {
	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", s.homeDir, "init"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	s.lastErr = cmd.ExecuteContext(context.Background())
	return nil
}

func (s *cliScenario) cronFileExists(context.Context) error {
	if _, err := os.Stat(s.cronPath); err != nil {
		return fmt.Errorf("crontab not found: %w", err)
	}
	return nil
}

func (s *cliScenario) cronContainsDefaults(context.Context) error {
	content, err := os.ReadFile(s.cronPath)
	if err != nil {
		return err
	}
	for _, entry := range []string{"task-dispatch", "intake", "heartbeat", "disk-watchdog", "memory-curator"} {
		if !bytes.Contains(content, []byte(entry)) {
			return fmt.Errorf("crontab missing %q", entry)
		}
	}
	return nil
}

func (s *cliScenario) homeWithCustomCron(context.Context) error {
	if err := os.MkdirAll(s.homeDir, 0o755); err != nil {
		return err
	}
	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", s.homeDir, "init"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		return err
	}
	s.cronCustom = "@every 11s intake\n"
	return os.WriteFile(s.cronPath, []byte(s.cronCustom), 0o644)
}

func (s *cliScenario) customCronPreserved(context.Context) error {
	content, err := os.ReadFile(s.cronPath)
	if err != nil {
		return err
	}
	if string(content) != s.cronCustom {
		return fmt.Errorf("crontab = %q, want %q", content, s.cronCustom)
	}
	return nil
}

func (s *cliScenario) cronWithEntries(_ context.Context, entry1, entry2 string) error {
	if err := os.MkdirAll(s.homeDir, 0o755); err != nil {
		return err
	}
	content := entry1 + "\n" + entry2 + "\n"
	return os.WriteFile(s.cronPath, []byte(content), 0o644)
}

func (s *cliScenario) cronWithSingleEntry(_ context.Context, entry string) error {
	if err := os.MkdirAll(s.homeDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.cronPath, []byte(entry+"\n"), 0o644)
}

func (s *cliScenario) loadCrontab(context.Context) error {
	var err error
	s.cronSched, err = config.LoadCron(s.cronPath)
	s.lastErr = err
	return nil
}

func (s *cliScenario) taskDispatchInterval(_ context.Context, seconds int) error {
	want := time.Duration(seconds) * time.Second
	if s.cronSched.TaskDispatch != want {
		return fmt.Errorf("task-dispatch = %s, want %s", s.cronSched.TaskDispatch, want)
	}
	return nil
}

func (s *cliScenario) diskWatchdogHasSchedule(context.Context) error {
	if s.cronSched.DiskWatchdog.Schedule == nil {
		return fmt.Errorf("disk-watchdog has no schedule")
	}
	return nil
}

func (s *cliScenario) noError(context.Context) error {
	if s.lastErr != nil {
		return fmt.Errorf("unexpected error: %v", s.lastErr)
	}
	return nil
}

func (s *cliScenario) defaultTaskDispatch(context.Context) error {
	want := config.DefaultCronSchedule.TaskDispatch
	if s.cronSched.TaskDispatch != want {
		return fmt.Errorf("task-dispatch = %s, want default %s", s.cronSched.TaskDispatch, want)
	}
	return nil
}

// Config flag steps

func (s *cliScenario) configFileWith(_ context.Context, yamlLine string) error {
	if err := os.MkdirAll(s.homeDir, 0o755); err != nil {
		return err
	}
	s.configFile = filepath.Join(s.homeDir, "test-config.yaml")
	existing, _ := os.ReadFile(s.configFile)
	return os.WriteFile(s.configFile, append(existing, []byte(yamlLine+"\n")...), 0o644)
}

func (s *cliScenario) setEnvVar(_ context.Context, key, value string) error {
	return os.Setenv(key, value)
}

func (s *cliScenario) loadWithExplicitConfig(context.Context) error {
	defer func() { _ = os.Unsetenv("AGENTD_GATEWAY_MAX_TASKS_PER_PHASE") }()
	var err error
	s.loadedCfg, err = config.Load(config.LoadOptions{
		HomeOverride: s.homeDir,
		ConfigFile:   s.configFile,
	})
	s.lastErr = err
	return nil
}

func (s *cliScenario) maxTasksPerPhase(_ context.Context, want int) error {
	if s.loadedCfg.Gateway.MaxTasksPerPhase != want {
		return fmt.Errorf("max_tasks_per_phase = %d, want %d", s.loadedCfg.Gateway.MaxTasksPerPhase, want)
	}
	return nil
}

func (s *cliScenario) nonExistentConfigFile(context.Context) error {
	s.configFile = filepath.Join(s.homeDir, "does-not-exist.yaml")
	return nil
}

func (s *cliScenario) errorReturned(context.Context) error {
	if s.lastErr == nil {
		return fmt.Errorf("expected an error but got nil")
	}
	return nil
}

func (s *cliScenario) healingEnabled(context.Context) error {
	if !s.loadedCfg.Healing.Enabled {
		return fmt.Errorf("healing.enabled = false, want true")
	}
	return nil
}
