package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadMCPServers_NewKey(t *testing.T) {
	t.Parallel()

	v := viper.New()
	v.SetConfigType("yaml")
	yaml := `
gateway:
  mcp_servers:
    - type: stdio
      name: mytool
      command: /usr/bin/mytool
`
	if err := v.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	servers, err := loadMCPServers(v)
	if err != nil {
		t.Fatalf("loadMCPServers() error = %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("loadMCPServers() len = %d, want 1", len(servers))
	}
	if servers[0].Name != "mytool" {
		t.Errorf("servers[0].Name = %q, want mytool", servers[0].Name)
	}
}

func TestLoadMCPServers_OldKeyFallback(t *testing.T) {
	t.Parallel()

	v := viper.New()
	v.SetConfigType("yaml")
	yaml := `
gateway:
  capabilities:
    - type: stdio
      name: legacytool
      command: /usr/bin/legacytool
`
	if err := v.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	servers, err := loadMCPServers(v)
	if err != nil {
		t.Fatalf("loadMCPServers() error = %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("loadMCPServers() len = %d, want 1 (fallback to old key)", len(servers))
	}
	if servers[0].Name != "legacytool" {
		t.Errorf("servers[0].Name = %q, want legacytool", servers[0].Name)
	}
}

func TestLoadMCPServers_OldKeyIgnoredWhenNewPresent(t *testing.T) {
	t.Parallel()

	v := viper.New()
	v.SetConfigType("yaml")
	yaml := `
gateway:
  mcp_servers:
    - type: stdio
      name: newtool
      command: /usr/bin/newtool
  capabilities:
    - type: stdio
      name: oldtool
      command: /usr/bin/oldtool
`
	if err := v.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	servers, err := loadMCPServers(v)
	if err != nil {
		t.Fatalf("loadMCPServers() error = %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("loadMCPServers() len = %d, want 1 (new key wins)", len(servers))
	}
	if servers[0].Name != "newtool" {
		t.Errorf("servers[0].Name = %q, want newtool (new key should win)", servers[0].Name)
	}
}

func TestLoadMCPServers_ExpandsAuthTokenEnv(t *testing.T) {
	t.Setenv("MCP_TOKEN", "secret-token")
	v := viper.New()
	v.SetConfigType("yaml")
	yaml := `
gateway:
  mcp_servers:
    - type: stdio
      name: securetool
      command: /usr/bin/securetool
      auth:
        type: bearer
        token: "${MCP_TOKEN}"
`
	if err := v.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	servers, err := loadMCPServers(v)
	if err != nil {
		t.Fatalf("loadMCPServers() error = %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("loadMCPServers() len = %d, want 1", len(servers))
	}
	if servers[0].Auth.Token != "secret-token" {
		t.Fatalf("servers[0].Auth.Token = %q, want secret-token", servers[0].Auth.Token)
	}
}
