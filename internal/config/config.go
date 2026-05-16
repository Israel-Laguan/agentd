package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agentd/internal/paths"

	"github.com/spf13/viper"
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
	Cron        CronSchedule
}

// LoadOptions controls how agentd process configuration is resolved.
type LoadOptions struct {
	HomeOverride string
	ConfigFile   string
}

// Load resolves agentd paths and reads optional config from <home>/config.yaml.
func Load(opts LoadOptions) (Config, error) {
	homeDir, err := ResolveHome(opts.HomeOverride)
	if err != nil {
		return Config{}, err
	}

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
	cron, err := LoadCron(cfg.CronPath)
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
