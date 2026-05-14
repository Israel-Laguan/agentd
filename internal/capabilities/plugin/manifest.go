package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Manifest describes a plugin's metadata, hooks, capabilities, and
// environment variable requirements. It is parsed from manifest.json
// inside each plugin directory.
type Manifest struct {
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Priority     int          `json:"priority"`
	Hooks        HookSpecs    `json:"hooks"`
	Capabilities []string     `json:"capabilities"`
	Env          EnvSpec      `json:"env"`
	Dir          string       `json:"-"`
}

// HookSpecs declares the hook scripts or identifiers a plugin provides.
type HookSpecs struct {
	PreToolUse  []HookEntry `json:"pre_tool_use"`
	PostToolUse []HookEntry `json:"post_tool_use"`
}

// HookEntry describes a single hook within a manifest.
type HookEntry struct {
	Name    string `json:"name"`
	Script  string `json:"script"`
	Timeout string `json:"timeout"`
	Policy  string `json:"policy"`
}

// EnvSpec declares required and optional environment variables.
type EnvSpec struct {
	Required []string `json:"required"`
	Optional []string `json:"optional"`
}

// ParseManifest reads and validates a manifest.json from the given
// plugin directory path. It returns ErrManifestNotFound if the file
// does not exist, or ErrManifestInvalid if parsing or validation
// fails.
func ParseManifest(dir string) (Manifest, error) {
	path := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{}, fmt.Errorf("%w: %s", ErrManifestNotFound, path)
		}
		return Manifest{}, fmt.Errorf("%w: read %s: %v", ErrManifestInvalid, path, err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}
	m.Dir = dir

	if err := validateManifest(m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func validateManifest(m Manifest) error {
	if m.Name == "" {
		return fmt.Errorf("%w: name is required", ErrManifestInvalid)
	}
	if m.Version == "" {
		return fmt.Errorf("%w: version is required", ErrManifestInvalid)
	}
	for i, h := range m.Hooks.PreToolUse {
		if err := validateHookEntry(h, "pre_tool_use", i); err != nil {
			return err
		}
	}
	for i, h := range m.Hooks.PostToolUse {
		if err := validateHookEntry(h, "post_tool_use", i); err != nil {
			return err
		}
	}
	return nil
}

func validateHookEntry(h HookEntry, phase string, idx int) error {
	if h.Name == "" {
		return fmt.Errorf(
			"%w: hooks.%s[%d].name is required",
			ErrManifestInvalid, phase, idx,
		)
	}
	if h.Policy != "" && h.Policy != "fail_open" && h.Policy != "fail_closed" {
		return fmt.Errorf(
			"%w: hooks.%s[%d].policy must be fail_open or fail_closed, got %q",
			ErrManifestInvalid, phase, idx, h.Policy,
		)
	}
	return nil
}
