package worker

import (
	"fmt"
	"testing"
	"time"

	"agentd/internal/models"
)

func testTieredParent() models.Task {
	return models.Task{
		BaseEntity:  models.BaseEntity{ID: "parent-1"},
		ProjectID:   "proj-1",
		Title:       "Refactor billing module",
		Description: "Split the billing module into smaller services.",
		Assignee:    models.TaskAssigneeSystem,
	}
}

func TestSplitIntoTieredDAG_StepOrderAndProfiles(t *testing.T) {
	t.Parallel()
	now := time.Now()
	children, _ := SplitIntoTieredDAG(testTieredParent(), now)
	if len(children) != 4 {
		t.Fatalf("len(children) = %d, want 4", len(children))
	}
	wantProfiles := []string{
		tieredStepProfile[TieredStepContext],
		tieredStepProfile[TieredStepDecision],
		tieredStepProfile[TieredStepExecute],
		tieredStepProfile[TieredStepVerify],
	}
	for i, want := range wantProfiles {
		if got := children[i].AgentID; got != want {
			t.Fatalf("children[%d].AgentID = %q, want %q", i, got, want)
		}
	}
}

func TestSplitIntoTieredDAG_ContextStartsReady_OthersPending(t *testing.T) {
	t.Parallel()
	now := time.Now()
	children, _ := SplitIntoTieredDAG(testTieredParent(), now)
	if children[0].State != models.TaskStateReady {
		t.Fatalf("context step state = %s, want READY", children[0].State)
	}
	for i := 1; i < len(children); i++ {
		if children[i].State != models.TaskStatePending {
			t.Fatalf("children[%d] state = %s, want PENDING", i, children[i].State)
		}
	}
}

func TestSplitIntoTieredDAG_DependencyChainIsSequential(t *testing.T) {
	t.Parallel()
	now := time.Now()
	children, _ := SplitIntoTieredDAG(testTieredParent(), now)
	if len(children[0].DependsOn) != 0 {
		t.Fatalf("context step DependsOn = %v, want none", children[0].DependsOn)
	}
	for i := 1; i < len(children); i++ {
		want := children[i-1].ID
		if got := children[i].DependsOn; len(got) != 1 || got[0] != want {
			t.Fatalf("children[%d].DependsOn = %v, want [%q]", i, got, want)
		}
	}
}

func TestSplitIntoTieredDAG_RelationsCoverSpawnedByAndDependsOn(t *testing.T) {
	t.Parallel()
	now := time.Now()
	parent := testTieredParent()
	children, relations := SplitIntoTieredDAG(parent, now)

	childIndex := make(map[string]int, len(children))
	for i, child := range children {
		childIndex[child.ID] = i
	}

	var spawnedBy, dependsOn int
	for _, rel := range relations {
		switch rel.RelationType {
		case models.TaskRelationSpawnedBy:
			spawnedBy++
			if rel.ParentTaskID != parent.ID {
				t.Fatalf("SPAWNED_BY relation parent = %q, want %q", rel.ParentTaskID, parent.ID)
			}
			if _, ok := childIndex[rel.ChildTaskID]; !ok {
				t.Fatalf("SPAWNED_BY relation child = %q not in returned children", rel.ChildTaskID)
			}
		case models.TaskRelationDependsOn:
			dependsOn++
			ci, okC := childIndex[rel.ChildTaskID]
			pi, okP := childIndex[rel.ParentTaskID]
			if !okC || !okP || ci != pi+1 {
				t.Fatalf("DEPENDS_ON relation %+v does not point to correct predecessor", rel)
			}
		default:
			t.Fatalf("unexpected relation type %q", rel.RelationType)
		}
	}
	if spawnedBy != len(children) {
		t.Fatalf("SPAWNED_BY relations = %d, want %d (one per child)", spawnedBy, len(children))
	}
	if dependsOn != len(children)-1 {
		t.Fatalf("DEPENDS_ON relations = %d, want %d", dependsOn, len(children)-1)
	}
}

func TestSplitIntoTieredDAG_InheritsProjectAndAssignee(t *testing.T) {
	t.Parallel()
	now := time.Now()
	parent := testTieredParent()
	children, _ := SplitIntoTieredDAG(parent, now)
	for i, child := range children {
		if child.ID == "" {
			t.Fatalf("children[%d].ID is empty", i)
		}
		if child.ProjectID != parent.ProjectID {
			t.Fatalf("children[%d].ProjectID = %q, want %q", i, child.ProjectID, parent.ProjectID)
		}
		if child.Assignee != parent.Assignee {
			t.Fatalf("children[%d].Assignee = %q, want %q", i, child.Assignee, parent.Assignee)
		}
	}
}

func TestSplitIntoTieredDAG_ChildTitlesReferenceStepKind(t *testing.T) {
	t.Parallel()
	now := time.Now()
	parent := testTieredParent()
	children, _ := SplitIntoTieredDAG(parent, now)
	wantPrefixes := []TieredStepKind{TieredStepContext, TieredStepDecision, TieredStepExecute, TieredStepVerify}
	for i, prefix := range wantPrefixes {
		want := fmt.Sprintf("%s:", prefix)
		if got := children[i].Title; len(got) < len(want) || got[:len(want)] != want {
			t.Fatalf("children[%d].Title = %q, want prefix %q", i, got, want)
		}
	}
}
