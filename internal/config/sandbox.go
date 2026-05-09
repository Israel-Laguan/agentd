package config

import (
	"time"

	"github.com/spf13/viper"
)

const (
	defaultSandboxInactivityTimeout = 60 * time.Second
	defaultSandboxWallTimeout       = 10 * time.Minute
	defaultSandboxKillGrace         = 2 * time.Second
	defaultSandboxMaxLogBytes       = 5 * 1024 * 1024
	defaultSandboxAddressSpaceBytes = 2 * 1024 * 1024 * 1024
	defaultSandboxCPULimitSeconds   = 600
	defaultSandboxOpenFilesLimit    = 1024
	defaultSandboxProcessesLimit    = 256
)

var defaultSandboxEnvAllowlist = []string{"PATH", "HOME", "LANG", "LC_ALL", "USER"}
var defaultSandboxExtraEnv = []string{"CI=true", "DEBIAN_FRONTEND=noninteractive", "NO_COLOR=1"}

type SandboxLimitsConfig struct {
	AddressSpaceBytes uint64
	CPUSeconds        uint64
	OpenFiles         uint64
	Processes         uint64
}

type SandboxConfig struct {
	InactivityTimeout time.Duration
	WallTimeout       time.Duration
	KillGrace         time.Duration
	MaxLogBytes       int
	EnvAllowlist      []string
	ExtraEnv          []string
	ScrubPatterns     []string
	Limits            SandboxLimitsConfig
}

func setSandboxDefaults(v *viper.Viper) {
	v.SetDefault("sandbox.inactivity_timeout", defaultSandboxInactivityTimeout.String())
	v.SetDefault("sandbox.wall_timeout", defaultSandboxWallTimeout.String())
	v.SetDefault("sandbox.kill_grace", defaultSandboxKillGrace.String())
	v.SetDefault("sandbox.max_log_bytes", defaultSandboxMaxLogBytes)
	v.SetDefault("sandbox.env_allowlist", defaultSandboxEnvAllowlist)
	v.SetDefault("sandbox.extra_env", defaultSandboxExtraEnv)
	v.SetDefault("sandbox.scrub_patterns", []string{})
	v.SetDefault("sandbox.limits.address_space_bytes", defaultSandboxAddressSpaceBytes)
	v.SetDefault("sandbox.limits.cpu_seconds", defaultSandboxCPULimitSeconds)
	v.SetDefault("sandbox.limits.open_files", defaultSandboxOpenFilesLimit)
	v.SetDefault("sandbox.limits.processes", defaultSandboxProcessesLimit)
}

func loadSandboxConfig(v *viper.Viper) SandboxConfig {
	cfg := SandboxConfig{
		InactivityTimeout: v.GetDuration("sandbox.inactivity_timeout"),
		WallTimeout:       v.GetDuration("sandbox.wall_timeout"),
		KillGrace:         v.GetDuration("sandbox.kill_grace"),
		MaxLogBytes:       v.GetInt("sandbox.max_log_bytes"),
		EnvAllowlist:      v.GetStringSlice("sandbox.env_allowlist"),
		ExtraEnv:          v.GetStringSlice("sandbox.extra_env"),
		ScrubPatterns:     v.GetStringSlice("sandbox.scrub_patterns"),
		Limits: SandboxLimitsConfig{
			AddressSpaceBytes: v.GetUint64("sandbox.limits.address_space_bytes"),
			CPUSeconds:        v.GetUint64("sandbox.limits.cpu_seconds"),
			OpenFiles:         v.GetUint64("sandbox.limits.open_files"),
			Processes:         v.GetUint64("sandbox.limits.processes"),
		},
	}
	if cfg.InactivityTimeout <= 0 {
		cfg.InactivityTimeout = defaultSandboxInactivityTimeout
	}
	if cfg.WallTimeout <= 0 {
		cfg.WallTimeout = defaultSandboxWallTimeout
	}
	if cfg.KillGrace <= 0 {
		cfg.KillGrace = defaultSandboxKillGrace
	}
	if cfg.MaxLogBytes <= 0 {
		cfg.MaxLogBytes = defaultSandboxMaxLogBytes
	}
	if len(cfg.EnvAllowlist) == 0 {
		cfg.EnvAllowlist = append([]string(nil), defaultSandboxEnvAllowlist...)
	}
	if len(cfg.ExtraEnv) == 0 {
		cfg.ExtraEnv = append([]string(nil), defaultSandboxExtraEnv...)
	}
	if cfg.Limits.AddressSpaceBytes == 0 {
		cfg.Limits.AddressSpaceBytes = defaultSandboxAddressSpaceBytes
	}
	if cfg.Limits.CPUSeconds == 0 {
		cfg.Limits.CPUSeconds = defaultSandboxCPULimitSeconds
	}
	if cfg.Limits.OpenFiles == 0 {
		cfg.Limits.OpenFiles = defaultSandboxOpenFilesLimit
	}
	if cfg.Limits.Processes == 0 {
		cfg.Limits.Processes = defaultSandboxProcessesLimit
	}
	return cfg
}
