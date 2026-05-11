package sandbox

import (
	"errors"
	"testing"

	"agentd/internal/models"
)

func TestValidateCommandPaths_Empty(t *testing.T) {
	err := validateCommandPaths("", "/workspace")
	if err != nil {
		t.Errorf("validateCommandPaths() error = %v", err)
	}
}

func TestValidateCommandPaths_Whitespace(t *testing.T) {
	err := validateCommandPaths("   ", "/workspace")
	if err != nil {
		t.Errorf("validateCommandPaths() error = %v", err)
	}
}

func TestValidateCommandPaths_HomePathTilde(t *testing.T) {
	err := validateCommandPaths("cat ~/file.txt", "/workspace")
	if err == nil {
		t.Error("expected error for home path")
	}
	if !IsSandboxViolation(err) {
		t.Error("expected ErrSandboxViolation")
	}
}

func TestValidateCommandPaths_HomePathDollar(t *testing.T) {
	err := validateCommandPaths("cat $HOME/file.txt", "/workspace")
	if err == nil {
		t.Error("expected error for $HOME")
	}
	if !IsSandboxViolation(err) {
		t.Error("expected ErrSandboxViolation")
	}
}

func TestValidateCommandPaths_DirectoryTraversal(t *testing.T) {
	err := validateCommandPaths("cat ../secret.txt", "/workspace")
	if err == nil {
		t.Error("expected error for ..")
	}
	if !IsSandboxViolation(err) {
		t.Error("expected ErrSandboxViolation")
	}
}

func TestValidateCommandPaths_DirectoryTraversalWindows(t *testing.T) {
	err := validateCommandPaths("type ..\\secret.txt", "/workspace")
	if err == nil {
		t.Error("expected error for ..\\")
	}
	if !IsSandboxViolation(err) {
		t.Error("expected ErrSandboxViolation")
	}
}

func TestValidateAbsolutePathTokens_Valid(t *testing.T) {
	err := validateAbsolutePathTokens("cat /workspace/file.txt", "/workspace")
	if err != nil {
		t.Errorf("validateAbsolutePathTokens() error = %v", err)
	}
}

func TestValidateAbsolutePathTokens_Escapes(t *testing.T) {
	err := validateAbsolutePathTokens("cat /etc/passwd", "/workspace")
	if err == nil {
		t.Error("expected error for path escaping workspace")
	}
	if !IsSandboxViolation(err) {
		t.Error("expected ErrSandboxViolation")
	}
}

func TestValidateAbsolutePathTokens_Invalid(t *testing.T) {
	err := validateAbsolutePathTokens("cat /invalid/\x00file", "/workspace")
	if err == nil {
		t.Error("expected error for invalid path")
	}
}

func TestValidateChangeDirTargets_Valid(t *testing.T) {
	err := validateChangeDirTargets("cd subdir", "/workspace")
	if err != nil {
		t.Errorf("validateChangeDirTargets() error = %v", err)
	}
}

func TestValidateChangeDirTargets_Escapes(t *testing.T) {
	err := validateChangeDirTargets("cd /etc", "/workspace")
	if err == nil {
		t.Error("expected error for cd escaping workspace")
	}
	if !IsSandboxViolation(err) {
		t.Error("expected ErrSandboxViolation")
	}
}

func TestValidateChangeDirTargets_Dot(t *testing.T) {
	err := validateChangeDirTargets("cd .", "/workspace")
	if err != nil {
		t.Errorf("validateChangeDirTargets() error = %v", err)
	}
}

func TestValidateChangeDirTargets_Empty(t *testing.T) {
	err := validateChangeDirTargets("cd", "/workspace")
	if err != nil {
		t.Errorf("validateChangeDirTargets() error = %v", err)
	}
}

func TestValidateChangeDirTargets_WithQuotes(t *testing.T) {
	err := validateChangeDirTargets(`cd "/workspace/subdir"`, "/workspace")
	if err != nil {
		t.Errorf("validateChangeDirTargets() error = %v", err)
	}
}

func TestValidateChangeDirTargets_Piped(t *testing.T) {
	err := validateChangeDirTargets("cat file.txt | cd /etc", "/workspace")
	if err == nil {
		t.Error("expected error for cd in pipe")
	}
	if !IsSandboxViolation(err) {
		t.Error("expected ErrSandboxViolation")
	}
}

func TestValidateChangeDirTargets_Semicolon(t *testing.T) {
	err := validateChangeDirTargets("cd /etc; ls", "/workspace")
	if err == nil {
		t.Error("expected error for cd after semicolon")
	}
	if !IsSandboxViolation(err) {
		t.Error("expected ErrSandboxViolation")
	}
}

func IsSandboxViolation(err error) bool {
	return errors.Is(err, models.ErrSandboxViolation)
}