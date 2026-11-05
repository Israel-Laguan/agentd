package acp

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
)

// writeFile writes content at path, creating its directories.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return out
}

// files lists every regular file under dir, relative to it.
func files(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			out = append(out, rel)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func envOf(argv []string) map[string]string {
	env := map[string]string{}
	for _, arg := range argv[1:] {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			break
		}
		env[key] = value
	}
	return env
}

func TestOpencodePresetRunsACPWithTheModelAndAskRules(t *testing.T) {
	t.Parallel()
	user, home := t.TempDir(), t.TempDir()
	creds := filepath.Join(user, ".local/share/opencode/auth.json")
	writeFile(t, creds, `{"opencode": {"type": "api", "key": "OC-KEY"}, "openrouter": {"type": "api", "key": "OR-KEY"}}`)
	writeFile(t, filepath.Join(user, ".cache/opencode/models.json"), `{}`)

	p := Presets["opencode"]
	argv, err := p.Home(Launch{Home: home, Executable: "/opt/opencode", Model: "opencode/free-1", Credentials: creds, UserHome: user})
	if err != nil {
		t.Fatal(err)
	}
	if argv[0] != "env" || !slices.Equal(argv[len(argv)-2:], []string{"/opt/opencode", "acp"}) {
		t.Fatalf("argv = %q", argv)
	}
	var config struct {
		Model      string            `json:"model"`
		Permission map[string]string `json:"permission"`
	}
	if err := json.Unmarshal([]byte(envOf(argv)["OPENCODE_CONFIG_CONTENT"]), &config); err != nil {
		t.Fatalf("OPENCODE_CONFIG_CONTENT: %v", err)
	}
	// The wildcard would ask for reads, and for agentd's own MCP tools too.
	want := map[string]string{"bash": "ask", "edit": "ask"}
	if config.Model != "opencode/free-1" || !maps.Equal(config.Permission, want) {
		t.Fatalf("config = %+v", config)
	}
	if !slices.Equal(p.Allow, []string{"opencode.ai:443"}) || !p.ModelOption || p.NoUsage {
		t.Fatalf("preset = %+v", p)
	}
	got := files(t, home)
	wantFiles := []string{".cache/opencode/models.json", ".local/share/opencode/auth.json"}
	if !slices.Equal(got, wantFiles) {
		t.Fatalf("home files = %q, want %q", got, wantFiles)
	}
	auth := readJSON(t, filepath.Join(home, ".local/share/opencode/auth.json"))
	if len(auth) != 1 || auth["opencode"] == nil {
		t.Fatalf("auth.json keeps %v; want only the model's provider", keys(auth))
	}
}

func TestOpencodePresetRefusesAModelWhoseProviderHasNoLogin(t *testing.T) {
	t.Parallel()
	creds := filepath.Join(t.TempDir(), "auth.json")
	writeFile(t, creds, `{"openrouter": {"type": "api", "key": "OR-KEY"}}`)
	_, err := Presets["opencode"].Home(Launch{Home: t.TempDir(), Model: "opencode/free-1", Credentials: creds})
	if err == nil || strings.Contains(err.Error(), "OR-KEY") {
		t.Fatalf("err = %v; want a refusal that names no secret", err)
	}
}

// runClineHome runs the cline preset on a login file holding cline and another
// provider, and returns the run's home and argv.
func runClineHome(t *testing.T) (string, []string) {
	t.Helper()
	home := t.TempDir()
	creds := filepath.Join(t.TempDir(), "providers.json")
	writeFile(t, creds, `{"version": 1, "lastUsedProvider": "openrouter", "modes": {}, "providers": {
		"cline": {"settings": {"provider": "cline", "model": "paid/default", "auth": {"accessToken": "CL-TOKEN"}}, "tokenSource": "oauth"},
		"openrouter": {"settings": {"provider": "openrouter", "auth": {"apiKey": "OR-KEY"}}}}}`)
	argv, err := Presets["cline"].Home(Launch{Home: home, Executable: "/opt/cline/bin/.cline", Model: "vendor/free-model:free", Credentials: creds, MCPURL: "http://127.0.0.1:3129/mcp/abc"})
	if err != nil {
		t.Fatal(err)
	}
	return home, argv
}

func TestClinePresetWritesItsSettingsAndRulesIntoTheHome(t *testing.T) {
	t.Parallel()
	home, argv := runClineHome(t)
	config := filepath.Join(home, ".cline")
	settings := filepath.Join(config, "data/settings/cline_mcp_settings.json")
	if !slices.Equal(argv[len(argv)-4:], []string{"/opt/cline/bin/.cline", "--acp", "--config", config}) ||
		envOf(argv)["CLINE_MCP_SETTINGS_PATH"] != settings || envOf(argv)["CLINE_MODEL"] != "vendor/free-model:free" {
		t.Fatalf("argv = %q", argv)
	}
	if p := Presets["cline"]; !slices.Equal(p.Allow, []string{"api.cline.bot:443", "data.cline.bot:443"}) || !p.ModelOption || !p.NoUsage {
		t.Fatalf("preset = %+v", p)
	}
	got := files(t, home)
	want := []string{".cline/data/settings/cline_mcp_settings.json", ".cline/data/settings/providers.json", ".cline/rules/agentd.md"}
	if !slices.Equal(got, want) {
		t.Fatalf("home files = %q, want %q", got, want)
	}
	server := readJSON(t, settings)["mcpServers"].(map[string]any)["agentd"].(map[string]any)
	if server["type"] != "streamableHttp" || server["url"] != "http://127.0.0.1:3129/mcp/abc" {
		t.Fatalf("agentd server = %v", server)
	}
	rules, err := os.ReadFile(filepath.Join(config, "rules/agentd.md"))
	if err != nil || !strings.Contains(string(rules), "ask_human") {
		t.Fatalf("rules = %q, %v", rules, err)
	}
}

// The login copy holds cline alone, and keeps the fields cline's schema
// checks as the user's file has them: a changed modes dropped the login.
func TestClinePresetCopiesOnlyItsLoginInTheUsersSchema(t *testing.T) {
	t.Parallel()
	home, _ := runClineHome(t)
	providers := readJSON(t, filepath.Join(home, ".cline/data/settings/providers.json"))
	byName, _ := providers["providers"].(map[string]any)
	if len(byName) != 1 || byName["cline"] == nil || providers["lastUsedProvider"] != "cline" {
		t.Fatalf("providers.json keeps %v, last used %v; want only cline", keys(byName), providers["lastUsedProvider"])
	}
	if modes, ok := providers["modes"].(map[string]any); !ok || len(modes) != 0 || providers["version"] != float64(1) {
		t.Fatalf("providers.json modes %v, version %v; want the user's", providers["modes"], providers["version"])
	}
}

func TestClinePresetNeedsTheSessionsMCPURL(t *testing.T) {
	t.Parallel()
	creds := filepath.Join(t.TempDir(), "providers.json")
	writeFile(t, creds, `{"providers": {"cline": {"settings": {}}}}`)
	if _, err := Presets["cline"].Home(Launch{Home: t.TempDir(), Model: "m:free", Credentials: creds}); err == nil {
		t.Fatal("cline started with no MCP url; it reads agentd's server only from its settings file")
	}
}

// The cline on PATH is a Node wrapper that writes into ~/.cline; the jail runs
// the compiled binary next to it.
func TestClineExecutableIsTheCompiledBinaryNextToTheWrapper(t *testing.T) {
	t.Parallel()
	dir, err := filepath.EvalSymlinks(t.TempDir()) // resolve the tempdir: it may be a symlink (macOS)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "pkg/bin/cline"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(dir, "pkg/bin/.cline"), "")
	if err := os.Symlink(filepath.Join(dir, "pkg/bin/cline"), filepath.Join(dir, "cline")); err != nil {
		t.Fatal(err)
	}
	got, err := Presets["cline"].Binary(filepath.Join(dir, "cline"))
	if err != nil || got != filepath.Join(dir, "pkg/bin/.cline") {
		t.Fatalf("Binary = %q, %v", got, err)
	}
}

func TestPresetConfigStartsTheHomesArgvWithTheSessionsMCPURL(t *testing.T) {
	t.Parallel()
	var seen Launch
	var ran []string
	p := Preset{ModelOption: true, Home: func(l Launch) ([]string, error) { seen = l; return []string{"agent", "acp"}, nil }}
	start := func(ctx context.Context, _ string, argv []string, _ bool) (*exec.Cmd, error) {
		ran = argv
		return exec.CommandContext(ctx, "true"), nil
	}
	cfg := p.Config(Launch{Model: "m"}, "/ws", true, start)
	if cfg.Model != "m" || cfg.Cwd != "/ws" || !cfg.ReadOnly {
		t.Fatalf("cfg = %+v", cfg)
	}
	servers := []acpsdk.McpServer{{Http: &acpsdk.McpServerHttpInline{Name: "agentd", Url: "http://jail/mcp/x"}}}
	if _, err := cfg.Command(context.Background(), "/ws", servers); err != nil {
		t.Fatal(err)
	}
	if seen.MCPURL != "http://jail/mcp/x" || !slices.Equal(ran, []string{"agent", "acp"}) {
		t.Fatalf("home saw %+v, ran %q", seen, ran)
	}
}

func keys(m map[string]any) []string { return slices.Sorted(maps.Keys(m)) }
