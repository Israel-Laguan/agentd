package acp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Presets are the agents a config's kind: acp may name. Measurements behind
// each value are in docs/acp-agents.md. Tests add fakes before anything reads
// the map; it is not locked.
var Presets = map[string]Preset{
	"opencode": {
		Command:     "opencode",
		Credentials: ".local/share/opencode/auth.json",
		Allow:       []string{"opencode.ai:443"},
		ModelOption: true,
		ModelCheck:  opencodeModelCheck,
		Home:        opencodeHome,
	},
	"cline": {
		Command:     "cline",
		Binary:      clineBinary,
		Credentials: ".cline/data/settings/providers.json",
		Allow:       []string{"api.cline.bot:443", "data.cline.bot:443"},
		ModelOption: true,
		NoUsage:     true,
		Home:        clineHome,
	},
}

// opencodeProvider is the one login provider the opencode preset runs: its
// models and the egress allowlist (opencode.ai:443) are its own.
const opencodeProvider = "opencode"

// opencodeModelCheck requires the model shape an opencode agent names: the
// provider its login and egress allowlist are named by, then the model id; a
// bare model would only fail when the run starts.
func opencodeModelCheck(model string) error {
	if _, _, ok := strings.Cut(model, "/"); !ok {
		return fmt.Errorf("acp: opencode model %q must be provider/model", model)
	}
	return nil
}

// opencodeSeed is what a cold opencode home downloads at start: the model
// catalog and ripgrep. Copied from the user's cache so neither host is listed.
var opencodeSeed = []string{".cache/opencode/models.json", ".cache/opencode/bin/rg"}

// opencodeHome logs in with only the model's provider, and sets the model and
// the rules that make opencode ask for its shell and writes: without them it
// never asks, and agentd's hooks never see them (opencode 2.2.5). Not "*",
// which also asks for reads and perhaps agentd's MCP tools, which the driver
// refuses; not webfetch, which the driver refuses before the hooks.
func opencodeHome(l Launch) ([]string, error) {
	provider, _, ok := strings.Cut(l.Model, "/")
	if !ok {
		return nil, fmt.Errorf("acp: opencode model %q names no provider (provider/model)", l.Model)
	}
	if provider != opencodeProvider {
		// The egress allowlist holds the opencode provider's host only, so a
		// model from another provider would be refused at egress; name its own.
		return nil, fmt.Errorf("acp: opencode runs %q only; model %q names provider %q", opencodeProvider, l.Model, provider)
	}
	var logins map[string]json.RawMessage
	if err := readLogin(l.Credentials, &logins); err != nil {
		return nil, err
	}
	login, ok := logins[provider]
	if !ok {
		return nil, fmt.Errorf("acp: %s has no login for provider %q", l.Credentials, provider)
	}
	if err := writeHomeJSON(l.Home, ".local/share/opencode/auth.json", map[string]json.RawMessage{provider: login}); err != nil {
		return nil, err
	}
	for _, rel := range opencodeSeed {
		if err := seed(l.UserHome, l.Home, rel); err != nil {
			return nil, fmt.Errorf("acp: seed opencode %s: %w", rel, err)
		}
	}
	config, err := json.Marshal(map[string]any{"model": l.Model, "permission": map[string]string{"bash": "ask", "edit": "ask"}})
	if err != nil {
		return nil, err
	}
	return []string{"env", "OPENCODE_CONFIG_CONTENT=" + string(config),
		"OPENCODE_DISABLE_AUTOUPDATE=1", "OPENCODE_DISABLE_MODELS_FETCH=1", l.Executable, "acp"}, nil
}

// clineProvider is the login cline gets: its own, whose hosts Allow lists.
// clinePassProvider is ClinePass on the same hosts and auth storage; when the
// user has it, it is used, since the usage-billed login refuses even free
// models once its credit balance is negative (SP-029).
const (
	clineProvider     = "cline"
	clinePassProvider = "cline-pass"
)

// clineRules makes cline ask instead of guessing: without it 6 of 10 runs of
// an ambiguous task reported Completed; with it, 0 of 10 (SP-026).
const clineRules = "# agentd\n\nNever guess a detail the task does not give you. If neither the task nor the workspace answers it, " +
	"call the ask_human tool from the agentd MCP server and end your turn without changing any file.\n"

// clineHome writes cline's config dir: the user's providers file cut to the
// one provider, agentd's MCP server (cline ignores session/new's), and the
// rules. The file's other fields stay as written: cline drops a file that
// fails its schema and then sees no login. The model is CLINE_MODEL, since
// the session ignores the file's (it ran cline's paid default).
// Cline's hub daemon starts inside the jail and dies with its PID namespace.
// shortcut: cline may refresh its token into the run's copy, which is then dropped; write it back if a rotated token ever breaks the user's login.
func clineHome(l Launch) ([]string, error) {
	if l.MCPURL == "" {
		return nil, fmt.Errorf("acp: cline needs agentd's MCP url for its settings file")
	}
	var file map[string]json.RawMessage
	if err := readLogin(l.Credentials, &file); err != nil {
		return nil, err
	}
	var providers map[string]json.RawMessage
	_ = json.Unmarshal(file["providers"], &providers) // a missing or odd field leaves no login
	login, ok := providers[clineProvider]
	if !ok {
		return nil, fmt.Errorf("acp: %s has no login for provider %q", l.Credentials, clineProvider)
	}
	keep, last := map[string]json.RawMessage{clineProvider: login}, clineProvider
	if pass, ok := providers[clinePassProvider]; ok {
		keep[clinePassProvider], last = pass, clinePassProvider
	}
	file["providers"], _ = json.Marshal(keep)
	file["lastUsedProvider"], _ = json.Marshal(last)
	config := filepath.Join(l.Home, ".cline")
	mcp := filepath.Join(config, "data/settings/cline_mcp_settings.json")
	writes := []struct {
		rel string
		v   any
	}{
		{".cline/data/settings/providers.json", file},
		// No autoApprove: cline's ACP mode asks before every tool regardless,
		// and the driver allows agentd's own (ownTool).
		{".cline/data/settings/cline_mcp_settings.json", map[string]any{"mcpServers": map[string]any{"agentd": map[string]any{
			"type": "streamableHttp", "url": l.MCPURL, "disabled": false}}}},
	}
	for _, w := range writes {
		if err := writeHomeJSON(l.Home, w.rel, w.v); err != nil {
			return nil, err
		}
	}
	if err := writeHomeFile(l.Home, ".cline/rules/agentd.md", []byte(clineRules)); err != nil {
		return nil, err
	}
	// ACP sessions take the provider from CLINE_PROVIDER, not lastUsedProvider,
	// and fall back to the provider's default model for an id it does not list.
	return []string{"env", "CLINE_MCP_SETTINGS_PATH=" + mcp, "CLINE_PROVIDER=" + last, "CLINE_MODEL=" + l.Model, "CLINE_NO_AUTO_UPDATE=1",
		l.Executable, "--acp", "--config", config}, nil
}

// clineBinary is the compiled cline next to the Node wrapper on PATH, which
// would write a CA bundle into ~/.cline (SP-025).
func clineBinary(found string) (string, error) {
	wrapper, err := filepath.EvalSymlinks(found)
	if err != nil {
		return "", err
	}
	binary := filepath.Join(filepath.Dir(wrapper), ".cline")
	if _, err := os.Stat(binary); err != nil {
		return "", fmt.Errorf("acp: no compiled cline next to %s: %w", wrapper, err)
	}
	return binary, nil
}
