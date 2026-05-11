package config

import (
	"testing"
)

func TestAPIConfig_Defaults(t *testing.T) {
	cfg := APIConfig{
		Address:          defaultAPIAddress,
		MaterializeToken: "",
	}
	if cfg.Address != defaultAPIAddress {
		t.Errorf("Address = %v, want %v", cfg.Address, defaultAPIAddress)
	}
	if cfg.MaterializeToken != "" {
		t.Errorf("MaterializeToken = %v, want empty", cfg.MaterializeToken)
	}
}

func TestAPIConfig_Custom(t *testing.T) {
	cfg := APIConfig{
		Address:          "0.0.0.0:9000",
		MaterializeToken: "secret-token",
	}
	if cfg.Address != "0.0.0.0:9000" {
		t.Errorf("Address = %v, want 0.0.0.0:9000", cfg.Address)
	}
	if cfg.MaterializeToken != "secret-token" {
		t.Errorf("MaterializeToken = %v, want secret-token", cfg.MaterializeToken)
	}
}

func TestDefaultAPIAddress(t *testing.T) {
	if defaultAPIAddress != "127.0.0.1:8765" {
		t.Errorf("defaultAPIAddress = %v, want 127.0.0.1:8765", defaultAPIAddress)
	}
}