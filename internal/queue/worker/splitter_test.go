package worker

import (
	"testing"

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
	children, _ := SplitIntoTieredDAG(testTieredParent())
	if len(children) != 4 {
		t.Fatalf("len(children) = %d, want 4", len(children))
	}
	wantProfiles := []string{"tier-context", "tier-decision", "tier-execute", "tier-verify"}
	for i, want := range wantProfiles {
		if got := children[i].AgentID; got != want {
			t.Fatalf("children[%d].AgentID = %q, want %q", i, got, want)
		}
	}
}

func TestSplitIntoTieredDAG_ContextStartsReady_OthersPending(t *testing.T) {
	t.Parallel()
	children, _ := SplitIntoTieredDAG(testTieredParent())
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
	children, _ := SplitIntoTieredDAG(testTieredParent())
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
	parent := testTieredParent()
	children, relations := SplitIntoTieredDAG(parent)

	var spawnedBy, dependsOn int
	for _, rel := range relations {
		switch rel.RelationType {
		case models.TaskRelationSpawnedBy:
			spawnedBy++
			if rel.ParentTaskID != parent.ID {
				t.Fatalf("SPAWNED_BY relation parent = %q, want %q", rel.ParentTaskID, parent.ID)
			}
		case models.TaskRelationDependsOn:
			dependsOn++
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
	parent := testTieredParent()
	children, _ := SplitIntoTieredDAG(parent)
	for i, child := range children {
		if child.ProjectID != parent.ProjectID {
			t.Fatalf("children[%d].ProjectID = %q, want %q", i, child.ProjectID, parent.ProjectID)
		}
		if child.Assignee != parent.Assignee {
			t.Fatalf("children[%d].Assignee = %q, want %q", i, child.Assignee, parent.Assignee)
		}
		if child.ID == "" {
			t.Fatalf("children[%d].ID is empty", i)
		}
	}
}

func TestSplitIntoTieredDAG_ChildTitlesReferenceStepKind(t *testing.T) {
	t.Parallel()
	parent := testTieredParent()
	children, _ := SplitIntoTieredDAG(parent)
	wantPrefixes := []string{"context:", "decision:", "execute:", "verify:"}
	for i, prefix := range wantPrefixes {
		if got := children[i].Title; len(got) < len(prefix) || got[:len(prefix)] != prefix {
			t.Fatalf("children[%d].Title = %q, want prefix %q", i, got, prefix)
		}
	}
}
