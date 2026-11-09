package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wskills "agentd/internal/agent/skills"
	"agentd/internal/models"
	"agentd/internal/stage"
	"agentd/internal/testutil"
)

func TestACPRequestIncludesCoordinatorContext(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	prefs := writeInstructionFixtures(t, dir)
	skills := filepath.Join(dir, ".agentd", "skills")
	if err := os.MkdirAll(skills, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skills, "deploy.md"), []byte("# Skill: Deploy\n\n## When This Applies\nDeployment, release\n\n## The Procedure\nRun deployment checks\n"), 0644); err != nil {
		t.Fatal(err)
	}
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "context", Tasks: []models.DraftTask{{Title: "Deploy", Description: "Deploy release"}}})
	if err != nil {
		t.Fatal(err)
	}
	task := tasks[0]
	_, reviews, err := store.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, []models.DraftTask{{Title: models.HITLSubtaskTitleReview + " draft", Assignee: models.TaskAssigneeHuman}})
	if err != nil {
		t.Fatal(err)
	}
	review := reviews[0]
	if err := store.AddComment(ctx, models.Comment{TaskID: review.ID, Author: models.CommentAuthorUser, Body: "Please add error handling"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateTaskState(ctx, review.ID, review.UpdatedAt, models.TaskStateFailed); err != nil {
		t.Fatal(err)
	}
	w := &Worker{store: store, instructionLoader: &InstructionLoader{ProjectFile: "AGENTS.md", UserPreferencesPath: prefs}, skillLoader: &wskills.SkillLoader{ProjectDir: ".agentd/skills"}, skillRouter: &wskills.SkillRouter{Threshold: 0, TopK: 3}, retriever: &recordingRetriever{memories: []models.Memory{lessonMemory("flaky test", "retry once")}}}
	w.attempts.recordFirst(task.ID, attemptFailure{command: "go test ./...", failure: "tests failed"})
	w.attempts.finish(task.ID, false)
	req := w.acpExecutionRequest(ctx, task, models.Project{WorkspacePath: dir}, models.AgentProfile{}, stage.AccessWrite)
	text := renderContext(req.Context)
	for _, want := range []string{"write tests first", "retry once", "Run deployment checks", "Please add error handling", "go test ./...", "tests failed"} {
		if !strings.Contains(text, want) {
			t.Errorf("ACP stage request missing %q", want)
		}
	}
}
