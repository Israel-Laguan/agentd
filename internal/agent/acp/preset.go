package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	acpsdk "github.com/coder/acp-go-sdk"
)

// Preset is how to run one external agent in the egress jail (T-042): the
// hosts it may reach, how its per-run home is laid out, and the argv that
// starts it there. Where the user's binary and login are, and the model, come
// from the config agent (Launch).
type Preset struct {
	// Command is the agent's name on PATH, for a config that names no executable.
	Command string
	// Binary maps the command found on PATH to the binary the jail runs; nil keeps it.
	Binary func(found string) (string, error)
	// Credentials is the user's login file under their home, for a config that names none.
	Credentials string
	// Allow is the egress allowlist, host:port: the model hosts only.
	// Cold-start downloads stay off it; Home seeds what they would fetch.
	Allow []string
	// AuthMethod, when set, is sent to authenticate. opencode and cline sign in outside ACP.
	AuthMethod string
	// ModelOption: session/new reports the model as its "model" config option,
	// so the driver can refuse a session on another (Config.Model).
	ModelOption bool
	// NoUsage: the agent reports no token usage (cline).
	NoUsage bool
	// Home writes the run's home and returns the argv run in the jail. The jail
	// clears the environment, so the argv sets the agent's own variables.
	Home func(Launch) ([]string, error)
}

// Launch is one run of a preset.
type Launch struct {
	Home        string // fresh per run, writable, HOME in the jail
	Executable  string
	Model       string
	Credentials string // the user's login file; only read
	UserHome    string // where seed caches are copied from
	// MCPURL is agentd's MCP server for the run's session, as the jail sees it.
	// Cline reads it from its settings file, not from session/new.
	MCPURL string
}

// Executable is the binary to run: configured, else Command on PATH through Binary.
func (p Preset) Executable(configured string) (string, error) {
	if configured != "" {
		return filepath.Abs(configured)
	}
	found, err := exec.LookPath(p.Command)
	if err != nil {
		return "", fmt.Errorf("acp: %s is not on PATH; set the agent's executable: %w", p.Command, err)
	}
	if p.Binary != nil {
		if found, err = p.Binary(found); err != nil {
			return "", err
		}
	}
	return filepath.Abs(found)
}

// Config is the driver config for one run of p in cwd. The process starts
// with start, once Home has written the run's home for the session's MCP server.
func (p Preset) Config(l Launch, cwd string, readOnly bool, start func(ctx context.Context, cwd string, argv []string, readOnly bool) (*exec.Cmd, error)) Config {
	cfg := Config{Cwd: cwd, ReadOnly: readOnly, AuthMethod: p.AuthMethod}
	if p.ModelOption {
		cfg.Model = l.Model
	}
	cfg.Command = func(ctx context.Context, cwd string, servers []acpsdk.McpServer) (*exec.Cmd, error) {
		l.MCPURL = agentdURL(servers)
		argv, err := p.Home(l)
		if err != nil {
			return nil, err
		}
		return start(ctx, cwd, argv, readOnly)
	}
	return cfg
}

// agentdURL is the url of agentd's http MCP server among servers, or "".
func agentdURL(servers []acpsdk.McpServer) string {
	for _, s := range servers {
		if s.Http != nil && s.Http.Name == "agentd" {
			return s.Http.Url
		}
	}
	return ""
}

// readLogin decodes the user's login file. Errors name the file, never its content.
func readLogin(path string, into any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("acp: read login %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("acp: login %s is not valid JSON", path)
	}
	return nil
}

// writeHomeFile writes data at rel under home, owner-only: it may hold a login.
func writeHomeFile(home, rel string, data []byte) error {
	path := filepath.Join(home, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func writeHomeJSON(home, rel string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeHomeFile(home, rel, data)
}

// seed copies rel from the user's home into the run's home, when present.
func seed(userHome, home, rel string) error {
	if userHome == "" {
		return nil
	}
	src, err := os.Open(filepath.Join(userHome, rel))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	dst := filepath.Join(home, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
