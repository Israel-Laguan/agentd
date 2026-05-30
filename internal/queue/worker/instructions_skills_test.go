package worker

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// SystemPromptBuilder.AddSkillBlock integration
// ---------------------------------------------------------------------------

func TestSystemPromptBuilder_AddSkillBlock(t *testing.T) {
	prompt := NewSystemPromptBuilder().
		WithGlobal("global instructions").
		AddSkillBlock("=== Skill: Deploy ===\nProcedure: push").
		WithTask("fix the deploy").
		Build()

	if !strings.Contains(prompt, "MATCHED SKILLS (contextual guidance):") {
		t.Fatal("prompt missing MATCHED SKILLS header")
	}
	if !strings.Contains(prompt, "=== Skill: Deploy ===") {
		t.Fatal("prompt missing skill block content")
	}

	// Verify ordering: skills appear after global but before task.
	globalIdx := strings.Index(prompt, "global instructions")
	skillIdx := strings.Index(prompt, "MATCHED SKILLS")
	taskIdx := strings.Index(prompt, "fix the deploy")
	if globalIdx >= skillIdx {
		t.Fatal("skills should appear after global instructions")
	}
	if skillIdx >= taskIdx {
		t.Fatal("skills should appear before task instructions")
	}
}

func TestSystemPromptBuilder_NoSkillBlocks(t *testing.T) {
	prompt := NewSystemPromptBuilder().
		WithGlobal("global").
		Build()
	if strings.Contains(prompt, "MATCHED SKILLS") {
		t.Fatal("prompt should not contain MATCHED SKILLS when none added")
	}
}
