package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

const envPrefix = "AGENTD"

func readDotEnv(path string) (map[string]string, error) {
	m, err := godotenv.Read(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return m, nil
}

func mergeDotEnv(base, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

func resolveHomeDir(homeOverride string, process, dotenv map[string]string) (string, error) {
	if homeOverride != "" {
		return filepath.Abs(homeOverride)
	}
	if v := envLookup(process, dotenv, "AGENTD_HOME"); v != "" {
		return filepath.Abs(v)
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(userHome, defaultHomeDirName), nil
}

func envLookup(process, dotenv map[string]string, key string) string {
	if process != nil {
		if v, ok := process[key]; ok {
			return v
		}
	}
	if dotenv != nil {
		return dotenv[key]
	}
	return ""
}

func loadDotEnvLayers(homeOverride string, process map[string]string) (string, map[string]string, error) {
	cwdEnv, err := readDotEnv(".env")
	if err != nil {
		return "", nil, fmt.Errorf("load .env from current directory: %w", err)
	}

	merged := mergeDotEnv(nil, cwdEnv)
	homeDir, err := resolveHomeDir(homeOverride, process, merged)
	if err != nil {
		return "", nil, err
	}

	homeEnv, err := readDotEnv(filepath.Join(homeDir, ".env"))
	if err != nil {
		return "", nil, fmt.Errorf("load .env from home directory: %w", err)
	}
	merged = mergeDotEnv(merged, homeEnv)

	resolvedHome, err := resolveHomeDir(homeOverride, process, merged)
	if err != nil {
		return "", nil, err
	}
	if resolvedHome != homeDir {
		homeEnv, err = readDotEnv(filepath.Join(resolvedHome, ".env"))
		if err != nil {
			return "", nil, fmt.Errorf("load .env from home directory: %w", err)
		}
		merged = mergeDotEnv(merged, homeEnv)
		homeDir = resolvedHome
	}

	return homeDir, merged, nil
}

func applyDotEnvToViper(v *viper.Viper, dotenv, process map[string]string) {
	if len(dotenv) == 0 {
		return
	}
	replacer := strings.NewReplacer("_", ".")
	for key, val := range dotenv {
		if !strings.HasPrefix(key, envPrefix+"_") {
			continue
		}
		if process != nil {
			if _, ok := process[key]; ok {
				continue
			}
		}
		configKey := strings.ToLower(strings.TrimPrefix(key, envPrefix+"_"))
		configKey = replacer.Replace(configKey)
		if configKey == "home" {
			continue
		}
		v.Set(configKey, val)
	}
}

func snapshotProcessEnv() map[string]string {
	snap := make(map[string]string)
	for _, kv := range os.Environ() {
		key, val, _ := strings.Cut(kv, "=")
		snap[key] = val
	}
	return snap
}
