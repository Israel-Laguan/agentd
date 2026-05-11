package config

import (
	"testing"
	"time"
)

func TestSandboxConfig_Defaults(t *testing.T) {
	cfg := SandboxConfig{
		InactivityTimeout: defaultSandboxInactivityTimeout,
		WallTimeout:       defaultSandboxWallTimeout,
		KillGrace:         defaultSandboxKillGrace,
		MaxLogBytes:       defaultSandboxMaxLogBytes,
		EnvAllowlist:      defaultSandboxEnvAllowlist,
		ExtraEnv:          defaultSandboxExtraEnv,
		Limits: SandboxLimitsConfig{
			AddressSpaceBytes: defaultSandboxAddressSpaceBytes,
			CPUSeconds:        defaultSandboxCPULimitSeconds,
			OpenFiles:         defaultSandboxOpenFilesLimit,
			Processes:         defaultSandboxProcessesLimit,
		},
	}
	if cfg.InactivityTimeout != 60*time.Second {
		t.Errorf("InactivityTimeout = %v, want 60s", cfg.InactivityTimeout)
	}
	if cfg.WallTimeout != 10*time.Minute {
		t.Errorf("WallTimeout = %v, want 10m", cfg.WallTimeout)
	}
	if cfg.KillGrace != 2*time.Second {
		t.Errorf("KillGrace = %v, want 2s", cfg.KillGrace)
	}
	if cfg.MaxLogBytes != 5*1024*1024 {
		t.Errorf("MaxLogBytes = %v, want 5MB", cfg.MaxLogBytes)
	}
	if len(cfg.EnvAllowlist) != 5 {
		t.Errorf("EnvAllowlist length = %v, want 5", len(cfg.EnvAllowlist))
	}
	if len(cfg.ExtraEnv) != 3 {
		t.Errorf("ExtraEnv length = %v, want 3", len(cfg.ExtraEnv))
	}
}

func TestSandboxConfig_LimitsDefaults(t *testing.T) {
	cfg := SandboxLimitsConfig{
		AddressSpaceBytes: defaultSandboxAddressSpaceBytes,
		CPUSeconds:        defaultSandboxCPULimitSeconds,
		OpenFiles:         defaultSandboxOpenFilesLimit,
		Processes:         defaultSandboxProcessesLimit,
	}
	if cfg.AddressSpaceBytes != 2*1024*1024*1024 {
		t.Errorf("AddressSpaceBytes = %v, want 2GB", cfg.AddressSpaceBytes)
	}
	if cfg.CPUSeconds != 600 {
		t.Errorf("CPUSeconds = %v, want 600", cfg.CPUSeconds)
	}
	if cfg.OpenFiles != 1024 {
		t.Errorf("OpenFiles = %v, want 1024", cfg.OpenFiles)
	}
	if cfg.Processes != 256 {
		t.Errorf("Processes = %v, want 256", cfg.Processes)
	}
}

func TestSandboxConfig_Custom(t *testing.T) {
	cfg := SandboxConfig{
		InactivityTimeout: 30 * time.Second,
		WallTimeout:       5 * time.Minute,
		KillGrace:         1 * time.Second,
		MaxLogBytes:       1024,
		EnvAllowlist:      []string{"PATH"},
		ExtraEnv:          []string{"TEST=1"},
		ScrubPatterns:     []string{"password"},
		Limits: SandboxLimitsConfig{
			AddressSpaceBytes: 1024 * 1024 * 1024,
			CPUSeconds:        300,
			OpenFiles:         512,
			Processes:        128,
		},
	}
	if cfg.InactivityTimeout != 30*time.Second {
		t.Errorf("InactivityTimeout = %v, want 30s", cfg.InactivityTimeout)
	}
	if cfg.Limits.AddressSpaceBytes != 1024*1024*1024 {
		t.Errorf("AddressSpaceBytes = %v, want 1GB", cfg.Limits.AddressSpaceBytes)
	}
}

func TestSandboxEnvAllowlist(t *testing.T) {
	expected := []string{"PATH", "HOME", "LANG", "LC_ALL", "USER"}
	if len(defaultSandboxEnvAllowlist) != len(expected) {
		t.Errorf("defaultSandboxEnvAllowlist length = %v, want %v", len(defaultSandboxEnvAllowlist), len(expected))
	}
}

func TestSandboxExtraEnv(t *testing.T) {
	if len(defaultSandboxExtraEnv) != 3 {
		t.Errorf("defaultSandboxExtraEnv length = %v, want 3", len(defaultSandboxExtraEnv))
	}
}