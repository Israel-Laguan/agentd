package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestLoad_AGENTD_HOME_FromCwdDotEnv(t *testing.T) {
	tmp := t.TempDir()
	customHome := filepath.Join(tmp, "custom-agentd")
	if err := os.WriteFile(filepath.Join(tmp, ".env"), []byte("AGENTD_HOME="+customHome+"\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	if old, ok := os.LookupEnv("AGENTD_HOME"); ok {
		t.Cleanup(func() { _ = os.Setenv("AGENTD_HOME", old) })
	} else {
		t.Cleanup(func() { _ = os.Unsetenv("AGENTD_HOME") })
	}
	_ = os.Unsetenv("AGENTD_HOME")

	cfg, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HomeDir != customHome {
		t.Errorf("HomeDir = %q, want %q", cfg.HomeDir, customHome)
	}
	wantDB := filepath.Join(customHome, "global.db")
	if cfg.DBPath != wantDB {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, wantDB)
	}
	wantProjects := filepath.Join(customHome, "projects")
	if cfg.ProjectsDir != wantProjects {
		t.Errorf("ProjectsDir = %q, want %q", cfg.ProjectsDir, wantProjects)
	}
}

func TestLoad_MalformedDotEnv(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, ".env"), []byte("KEY=\"unterminated\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	_, err = Load(LoadOptions{})
	if err == nil {
		t.Fatal("Load() error = nil, want malformed .env error")
	}
	if !strings.Contains(err.Error(), "load .env from current directory") {
		t.Fatalf("Load() error = %v, want current-directory .env wrapper", err)
	}
}

func TestLoad_ProcessEnvOverridesHomeDotEnv(t *testing.T) {
	tmp := t.TempDir()
	processHome := filepath.Join(tmp, "process-home")
	fileHome := filepath.Join(tmp, "file-home")
	if err := os.MkdirAll(processHome, 0o755); err != nil {
		t.Fatalf("mkdir process-home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(processHome, ".env"), []byte("AGENTD_HOME="+fileHome+"\n"), 0o644); err != nil {
		t.Fatalf("write home .env: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	if old, ok := os.LookupEnv("AGENTD_HOME"); ok {
		t.Cleanup(func() { _ = os.Setenv("AGENTD_HOME", old) })
	} else {
		t.Cleanup(func() { _ = os.Unsetenv("AGENTD_HOME") })
	}
	t.Setenv("AGENTD_HOME", processHome)

	cfg, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HomeDir != processHome {
		t.Errorf("HomeDir = %q, want %q (process env should win over home .env)", cfg.HomeDir, processHome)
	}
	wantDB := filepath.Join(processHome, "global.db")
	if cfg.DBPath != wantDB {
		t.Errorf("DBPath = %q, want %q (derived paths should follow effective home)", cfg.DBPath, wantDB)
	}
}

func TestLoad_HomeDotEnvAfterAGENTD_HOMEOverride(t *testing.T) {
	tmp := t.TempDir()
	homeB := filepath.Join(tmp, "home-b")
	homeC := filepath.Join(tmp, "home-c")
	if err := os.MkdirAll(homeC, 0o755); err != nil {
		t.Fatalf("mkdir home-c: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".env"), []byte("AGENTD_HOME="+homeB+"\n"), 0o644); err != nil {
		t.Fatalf("write cwd .env: %v", err)
	}
	if err := os.MkdirAll(homeB, 0o755); err != nil {
		t.Fatalf("mkdir home-b: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeB, ".env"), []byte("AGENTD_HOME="+homeC+"\n"), 0o644); err != nil {
		t.Fatalf("write home-b .env: %v", err)
	}
	const marker = "MARKER_FROM_HOME_C"
	if err := os.WriteFile(filepath.Join(homeC, ".env"), []byte(marker+"=loaded\n"), 0o644); err != nil {
		t.Fatalf("write home-c .env: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	if old, ok := os.LookupEnv("AGENTD_HOME"); ok {
		t.Cleanup(func() { _ = os.Setenv("AGENTD_HOME", old) })
	} else {
		t.Cleanup(func() { _ = os.Unsetenv("AGENTD_HOME") })
	}
	_ = os.Unsetenv("AGENTD_HOME")
	t.Cleanup(func() { _ = os.Unsetenv(marker) })
	_ = os.Unsetenv(marker)

	cfg, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HomeDir != homeC {
		t.Errorf("HomeDir = %q, want %q", cfg.HomeDir, homeC)
	}
}

func TestLoad_HomeRedirect_DropsIntermediateHomeDotEnv(t *testing.T) {
	tmp := t.TempDir()
	homeB := filepath.Join(tmp, "home-b")
	homeC := filepath.Join(tmp, "home-c")
	if err := os.MkdirAll(homeC, 0o755); err != nil {
		t.Fatalf("mkdir home-c: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".env"), []byte("AGENTD_HOME="+homeB+"\n"), 0o644); err != nil {
		t.Fatalf("write cwd .env: %v", err)
	}
	if err := os.MkdirAll(homeB, 0o755); err != nil {
		t.Fatalf("mkdir home-b: %v", err)
	}
	staleAddr := "1.2.3.4:9999"
	homeBEnv := "AGENTD_HOME=" + homeC + "\nAGENTD_API_ADDRESS=" + staleAddr + "\n"
	if err := os.WriteFile(filepath.Join(homeB, ".env"), []byte(homeBEnv), 0o644); err != nil {
		t.Fatalf("write home-b .env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeC, ".env"), []byte("AGENTD_API_ADDRESS="+defaultAPIAddress+"\n"), 0o644); err != nil {
		t.Fatalf("write home-c .env: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	if old, ok := os.LookupEnv("AGENTD_HOME"); ok {
		t.Cleanup(func() { _ = os.Setenv("AGENTD_HOME", old) })
	} else {
		t.Cleanup(func() { _ = os.Unsetenv("AGENTD_HOME") })
	}
	_ = os.Unsetenv("AGENTD_HOME")

	cfg, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HomeDir != homeC {
		t.Errorf("HomeDir = %q, want %q", cfg.HomeDir, homeC)
	}
	if cfg.API.Address != defaultAPIAddress {
		t.Errorf("API.Address = %q, want %q (intermediate home .env must not leak)", cfg.API.Address, defaultAPIAddress)
	}
	if cfg.API.Address == staleAddr {
		t.Errorf("API.Address = %q from intermediate home-b .env leaked into config", staleAddr)
	}
}

func TestApplyDotEnvToViper_UnderscoreKeys(t *testing.T) {
	v := viper.New()
	setGatewayDefaults(v)
	applyDotEnvToViper(v, map[string]string{
		"AGENTD_GATEWAY_OPENAI_API_KEY": "sk-test",
	}, nil)
	if got := v.GetString("gateway.openai.api_key"); got != "sk-test" {
		t.Errorf("gateway.openai.api_key = %q, want sk-test", got)
	}
	if got := v.GetString("gateway.openai.api.key"); got != "" {
		t.Errorf("gateway.openai.api.key = %q, want empty (naive underscore mapping)", got)
	}
}

func TestLoad_HomeOverrideBeatsCwdDotEnv(t *testing.T) {
	tmp := t.TempDir()
	fromEnv := filepath.Join(tmp, "from-env")
	explicit := filepath.Join(tmp, "explicit")
	if err := os.MkdirAll(explicit, 0o755); err != nil {
		t.Fatalf("mkdir explicit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".env"), []byte("AGENTD_HOME="+fromEnv+"\n"), 0o644); err != nil {
		t.Fatalf("write cwd .env: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	if old, ok := os.LookupEnv("AGENTD_HOME"); ok {
		t.Cleanup(func() { _ = os.Setenv("AGENTD_HOME", old) })
	} else {
		t.Cleanup(func() { _ = os.Unsetenv("AGENTD_HOME") })
	}
	_ = os.Unsetenv("AGENTD_HOME")

	cfg, err := Load(LoadOptions{HomeOverride: explicit})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HomeDir != explicit {
		t.Errorf("HomeDir = %q, want %q", cfg.HomeDir, explicit)
	}
	wantDB := filepath.Join(explicit, "global.db")
	if cfg.DBPath != wantDB {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, wantDB)
	}
	wantCron := filepath.Join(explicit, cronFileName)
	if cfg.CronPath != wantCron {
		t.Errorf("CronPath = %q, want %q", cfg.CronPath, wantCron)
	}
}

func TestLoad_NoEnvLeakAfterReturn(t *testing.T) {
	tmp := t.TempDir()
	const marker = "AGENTD_LOAD_LEAK_MARKER"
	if err := os.WriteFile(filepath.Join(tmp, ".env"), []byte(marker+"=loaded\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(marker) })
	_ = os.Unsetenv(marker)

	homeDir := filepath.Join(tmp, "agentd")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if _, err := Load(LoadOptions{HomeOverride: homeDir}); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := os.LookupEnv(marker); ok {
		t.Errorf("%s leaked into process env after Load() returned", marker)
	}
}
