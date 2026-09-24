package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/kanban"
)

func TestProjectCreateCreatesWorkspace(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	t.Setenv("AGENTD_GATEWAY_OPENAI_API_KEY", "test-key")
	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", home, "project", "create", "--name", "hello", "--description", "Create hello.txt"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("project create error = %v", err)
	}
	workspace := outputValue(t, output.String(), "workspace")
	if _, err := os.Stat(workspace); err != nil {
		t.Fatalf("workspace not created: %v", err)
	}
	if !strings.Contains(output.String(), "task_id=") {
		t.Fatalf("output missing task_id: %s", output.String())
	}
	if !filepath.IsAbs(workspace) {
		t.Fatalf("workspace = %q, want absolute path", workspace)
	}
	projectID := outputValue(t, output.String(), "project_id")
	store, err := kanban.OpenStore(filepath.Join(home, "global.db"), filepath.Join(home, "projects"))
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	project, err := store.GetProject(context.Background(), projectID)
	if err != nil {
		t.Fatalf("get reopened project: %v", err)
	}
	if project.WorkspacePath != workspace {
		t.Fatalf("reopened workspace_path = %q, printed workspace = %q", project.WorkspacePath, workspace)
	}
}

func outputValue(t *testing.T, output, key string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if value, ok := strings.CutPrefix(line, key+"="); ok {
			return value
		}
	}
	t.Fatalf("output missing %s: %s", key, output)
	return ""
}
