package models

import (
	"reflect"
	"strings"
	"testing"
)

func hostReportPlan() DraftPlan {
	return DraftPlan{ProjectName: "host", Tasks: []DraftTask{
		{ReferenceID: "os", Title: "Collect OS info", Description: "Run uname -a and save the output to os_info.txt"},
		{ReferenceID: "cpu", Title: "Collect CPU info", Description: "Run lscpu > cpu_info.txt"},
		{ReferenceID: "report", Title: "Build report", Description: "Create report.md summarizing os_info.txt and cpu_info.txt"},
	}}
}

// The model omitted depends_on; Validate must supply it, not the prompt.
func TestValidateAddsDependsOnForConsumedFiles(t *testing.T) {
	t.Parallel()
	plan := hostReportPlan()
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	if got, want := plan.Tasks[2].DependsOn, []string{"os", "cpu"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("report depends_on = %v, want %v", got, want)
	}
	if len(plan.Tasks[0].DependsOn)+len(plan.Tasks[1].DependsOn) != 0 {
		t.Fatalf("producers gained dependencies: %v %v", plan.Tasks[0].DependsOn, plan.Tasks[1].DependsOn)
	}
}

func TestValidateKeepsExistingDependsOnWithoutDuplicates(t *testing.T) {
	t.Parallel()
	plan := hostReportPlan()
	plan.Tasks[2].DependsOn = []string{"os"}
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	if got, want := plan.Tasks[2].DependsOn, []string{"os", "cpu"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("report depends_on = %v, want %v", got, want)
	}
}

func TestValidateGivesProducerWithoutIDAReference(t *testing.T) {
	t.Parallel()
	plan := hostReportPlan()
	plan.Tasks[0].ReferenceID = ""
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	id := plan.Tasks[0].ID()
	if id == "" {
		t.Fatal("producer still has no id")
	}
	if !containsString(plan.Tasks[2].DependsOn, id) {
		t.Fatalf("report depends_on = %v, want it to include %q", plan.Tasks[2].DependsOn, id)
	}
}

func TestValidateLeavesIndependentTasksAlone(t *testing.T) {
	t.Parallel()
	plan := DraftPlan{ProjectName: "p", Tasks: []DraftTask{
		{ReferenceID: "a", Title: "A", Description: "Save uname output to a.txt"},
		{ReferenceID: "b", Title: "B", Description: "Save lscpu output to b.txt"},
	}}
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	if len(plan.Tasks[0].DependsOn)+len(plan.Tasks[1].DependsOn) != 0 {
		t.Fatalf("unexpected dependencies: %v %v", plan.Tasks[0].DependsOn, plan.Tasks[1].DependsOn)
	}
}

// Naming a file as the report target is not a read of the collected files.
func TestValidateDoesNotTreatTheOutputFileAsAnInput(t *testing.T) {
	t.Parallel()
	plan := hostReportPlan()
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	for _, task := range plan.Tasks[:2] {
		if containsString(task.DependsOn, "report") {
			t.Fatalf("%s depends on report", task.ID())
		}
	}
}

func TestValidateRejectsFileDependencyCycle(t *testing.T) {
	t.Parallel()
	plan := DraftPlan{ProjectName: "p", Tasks: []DraftTask{
		{ReferenceID: "a", Title: "A", Description: "Read b.txt and save the result to a.txt"},
		{ReferenceID: "b", Title: "B", Description: "Read a.txt and write it to b.txt", DependsOn: []string{"a"}},
	}}
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("Validate() = %v, want a cycle error", err)
	}
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
