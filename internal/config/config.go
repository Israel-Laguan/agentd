package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"

	"agentd/internal/paths"
)

const (
	defaultHomeDirName = ".agentd"
	dbFileName         = "global.db"
	projectsDirName    = "projects"
	uploadsDirName     = "uploads"
	archivesDirName    = "archives"
	cronFileName       = "agentd.crontab"
)

// Config contains local agentd filesystem paths and loaded config values.
type Config struct {
	HomeDir     string
	DBPath      string
	ProjectsDir string
	UploadsDir  string
	ArchivesDir string
	CronPath    string
	API         APIConfig
	Gateway     GatewayConfig
	Sandbox     SandboxConfig
	Healing     HealingConfig
	Breaker     BreakerConfig
	Disk        DiskConfig
	Heartbeat   HeartbeatConfig
	Librarian   LibrarianConfig
	Queue       QueueConfig
	Agentic     AgenticConfig
	Channel     ChannelConfig
	Cron        CronSchedule
}

// LoadOptions controls how agentd process configuration is resolved.
type LoadOptions struct {
	HomeOverride string
	ConfigFile   string
}

// Load resolves agentd paths and reads optional config from <home>/config.yaml.
// Environment variables are seeded from .env (CWD) and ~/.agentd/.env before
// config is read; existing process env vars always take precedence.
func Load(opts LoadOptions) (Config, error) {
	originalEnv := snapshotProcessEnv()
	defer restoreProcessEnv(originalEnv)

	// Seed env from project-local .env first so AGENTD_HOME can influence ResolveHome.
	if err := godotenv.Load(".env"); err != nil && !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("load .env from current directory: %w", err)
	}

	homeDir, err := ResolveHome(opts.HomeOverride)
	if err != nil {
		return Config{}, err
	}

	// Home-level .env may override AGENTD_HOME (and other vars) from the CWD .env.
	if err := godotenv.Overload(filepath.Join(homeDir, ".env")); err != nil && !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("load .env from home directory: %w", err)
	}
	reapplySnapshotEnv(originalEnv)
	resolvedHome, err := ResolveHome(opts.HomeOverride)
	if err != nil {
		return Config{}, err
	}
	if resolvedHome != homeDir {
		if err := godotenv.Overload(filepath.Join(resolvedHome, ".env")); err != nil && !os.IsNotExist(err) {
			return Config{}, fmt.Errorf("load .env from home directory: %w", err)
		}
	}
	homeDir = resolvedHome

	restoreProcessEnv(originalEnv)

	// Re-seed env from .env files for config hydration (home already resolved).
	if err := godotenv.Load(".env"); err != nil && !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("load .env from current directory: %w", err)
	}
	if err := godotenv.Overload(filepath.Join(homeDir, ".env")); err != nil && !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("load .env from home directory: %w", err)
	}
	reapplySnapshotEnv(originalEnv)

	cfg := baseConfig(homeDir)
	v := newConfigViper(cfg, homeDir, opts.ConfigFile)
	if err := readConfig(v, opts.ConfigFile); err != nil {
		return Config{}, err
	}

	return hydrateConfig(cfg, v)
}

func baseConfig(homeDir string) Config {
	return Config{
		HomeDir:     homeDir,
		DBPath:      filepath.Join(homeDir, dbFileName),
		ProjectsDir: filepath.Join(homeDir, projectsDirName),
		UploadsDir:  filepath.Join(homeDir, uploadsDirName),
		ArchivesDir: filepath.Join(homeDir, archivesDirName),
		CronPath:    filepath.Join(homeDir, cronFileName),
	}
}

func newConfigViper(cfg Config, homeDir, configFile string) *viper.Viper {
	v := viper.New()
	if configFile != "" {
		v.SetConfigFile(configFile)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(homeDir)
	}
	v.SetEnvPrefix("AGENTD")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.SetDefault("home", cfg.HomeDir)
	v.Set("home", cfg.HomeDir)
	v.SetDefault("db_path", cfg.DBPath)
	v.SetDefault("projects_dir", cfg.ProjectsDir)
	v.SetDefault("uploads_dir", cfg.UploadsDir)
	setAPIDefaults(v)
	setGatewayDefaults(v)
	setSandboxDefaults(v)
	setHealingDefaults(v)
	setBreakerDefaults(v)
	setDiskDefaults(v)
	setHeartbeatDefaults(v)
	setLibrarianDefaults(v)
	setQueueDefaults(v)
	setAgenticDefaults(v)
	setChannelDefaults(v)
	return v
}

func readConfig(v *viper.Viper, configFile string) error {
	if err := v.ReadInConfig(); err != nil {
		if !isConfigNotFound(err) {
			return fmt.Errorf("read config: %w", err)
		}
	}
	if configFile != "" {
		if err := overrideFromExplicitConfig(v, configFile); err != nil {
			return fmt.Errorf("read explicit config: %w", err)
		}
	}
	return nil
}

func hydrateConfig(cfg Config, v *viper.Viper) (Config, error) {
	var err error
	cfg.HomeDir = v.GetString("home")
	cfg.DBPath = v.GetString("db_path")
	cfg.ProjectsDir = v.GetString("projects_dir")
	cfg.UploadsDir = v.GetString("uploads_dir")
	cfg.CronPath = filepath.Join(cfg.HomeDir, cronFileName)
	cfg.API = loadAPIConfig(v)
	cfg.Gateway = loadGatewayConfig(v)
	cfg.Sandbox = loadSandboxConfig(v)
	cfg.Healing = loadHealingConfig(v)
	cfg.Breaker = loadBreakerConfig(v)
	cfg.Disk = loadDiskConfig(v)
	cfg.Heartbeat = loadHeartbeatConfig(v)
	cfg.Librarian = loadLibrarianConfig(v)
	cfg.Queue = loadQueueConfig(v)
	cfg.Queue.Skills.GlobalDir = resolveSkillsGlobalDir(cfg.HomeDir, cfg.Queue.Skills.GlobalDir)
	cfg.Agentic, err = loadAgenticConfig(v)
	if err != nil {
		return Config{}, err
	}
	cfg.Agentic.Audit.Path = ResolveAuditPath(cfg.HomeDir, cfg.Agentic.Audit.Path)
	cfg.Agentic.PromptTemplatesPath = ResolvePromptTemplatesPath(cfg.HomeDir, cfg.Agentic.PromptTemplatesPath)
	cfg.Channel = loadChannelConfig(v)
	var cron CronSchedule
	cron, err = LoadCron(cfg.CronPath)
	if err != nil {
		return Config{}, err
	}
	cfg.Cron = cron
	return cfg, nil
}

// resolveSkillsGlobalDir normalizes queue.skills.global_dir after viper read.
// Empty input returns empty. A "~/..." prefix is expanded via paths.ExpandTildePrefix
// (UserHomeDir, then HOME, then USERPROFILE; unchanged if none resolve).
// Absolute paths are returned unchanged. Any other value is joined with homeDir.
func resolveSkillsGlobalDir(homeDir, raw string) string {
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "~/") {
		return paths.ExpandTildePrefix(raw)
	}
	if filepath.IsAbs(raw) {
		return raw
	}
	return filepath.Join(homeDir, raw)
}

// ResolveHome returns the agentd home directory from explicit input,
// AGENTD_HOME, or ~/.agentd.
func ResolveHome(homeOverride string) (string, error) {
	if homeOverride != "" {
		return filepath.Abs(homeOverride)
	}
	if envHome := os.Getenv("AGENTD_HOME"); envHome != "" {
		return filepath.Abs(envHome)
	}

	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(userHome, defaultHomeDirName), nil
}

// EnsureDirs creates the local agentd directory tree.
func EnsureDirs(cfg Config) error {
	for _, dir := range []string{cfg.HomeDir, cfg.ProjectsDir, cfg.UploadsDir, cfg.ArchivesDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	return nil
}

func isConfigNotFound(err error) bool {
	if _, ok := err.(viper.ConfigFileNotFoundError); ok {
		return true
	}
	return false
}

func snapshotProcessEnv() map[string]string {
	snap := make(map[string]string)
	for _, kv := range os.Environ() {
		key, val, _ := strings.Cut(kv, "=")
		snap[key] = val
	}
	return snap
}

func reapplySnapshotEnv(snap map[string]string) {
	for key, val := range snap {
		_ = os.Setenv(key, val)
	}
}

func restoreProcessEnv(snap map[string]string) {
	for key, val := range snap {
		_ = os.Setenv(key, val)
	}
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if _, ok := snap[key]; !ok {
			_ = os.Unsetenv(key)
		}
	}
}
