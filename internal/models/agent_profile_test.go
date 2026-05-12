package models

import (
	"testing"
)

// TestAgentProfile_DefaultAgenticModeIsFalse verifies that the default value
// of AgenticMode is false (Requirements 2.1, 2.2)
func TestAgentProfile_DefaultAgenticModeIsFalse(t *testing.T) {
	// Zero value of AgentProfile should have AgenticMode as false
	var profile AgentProfile

	if profile.AgenticMode != false {
		t.Errorf("AgenticMode default = %v, want false", profile.AgenticMode)
	}
}

// TestAgentProfile_AgenticModeCanBeSet verifies that AgenticMode can be
// configured (Requirements 2.1, 7)
func TestAgentProfile_AgenticModeCanBeSet(t *testing.T) {
	profile := AgentProfile{
		ID:           "test-agent",
		Name:         "Test Agent",
		Provider:     "openai",
		Model:        "gpt-4",
		Temperature:  0.7,
		AgenticMode:  true,
	}

	if !profile.AgenticMode {
		t.Errorf("AgenticMode = %v, want true", profile.AgenticMode)
	}

	// Verify it can be set back to false
	profile.AgenticMode = false
	if profile.AgenticMode {
		t.Errorf("AgenticMode = %v, want false after setting", profile.AgenticMode)
	}
}