package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

func TestACPFalseCompletionIsRequeuedWithGoldenFeedback(t *testing.T) {
	ctx := context.Background()
	script := fakeagent.Script(report(map[string]any{"summary": "done"}))
	w, store, task, project, profile, _, _ := acpFixture(t, script, false)
	// Decode through the public task representation so this test describes the
	// requested API as well as the controller's behavior.
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	fields["success_criteria"] = json.RawMessage(`["tests pass"]`)
	fields["golden_criteria"] = json.RawMessage(`[{"criterion":"tests pass","check_command":"test -f proof"}]`)
	data, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatal(err)
	}
	w.sandbox = fixedResultSandbox{result: sandbox.Result{Success: false, ExitCode: 1, Stderr: "proof is missing"}}
	w.sink = &storeEventSink{store: store}
	w.dispatchProcess(ctx, task, project, profile)
	got := mustGetTask(t, store, task.ID)
	if got.State != models.TaskStateReady {
		t.Fatalf("state = %s, want READY after failed golden check", got.State)
	}
	messages, err := w.buildStageContext(ctx, got, project, profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if strings.Contains(message.Content, "proof is missing") {
			return
		}
	}
	t.Fatalf("retry prompt did not contain check output: %+v", messages)
}

type goldenTestSandbox struct {
	results []map[string]bool
	checks  int
}

func (s *goldenTestSandbox) Execute(_ context.Context, p sandbox.Payload) (sandbox.Result, error) {
	cycle := s.checks / 4
	s.checks++
	if cycle >= len(s.results) {
		cycle = len(s.results) - 1
	}
	if s.results[cycle][p.Command] {
		return sandbox.Result{Success: true}, nil
	}
	return sandbox.Result{ExitCode: 1, Stderr: "check failed: " + p.Command}, nil
}

// A fake ACP agent repeatedly claims completion while checks show either
// progress or an unchanged set of failures.
func TestACPGoldenChecksAndCaps(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		results                      []map[string]bool
		cycleCap, stallCap, attempts int
		want                         models.TaskState
	}{
		{"pass", []map[string]bool{{"one": true, "two": true, "three": true, "four": true}}, 4, 2, 1, models.TaskStateCompleted},
		{"cycle cap with progress", []map[string]bool{{"one": true}, {"one": true, "two": true}, {"one": true, "two": true, "three": true}}, 3, 2, 3, models.TaskStateFailedRequiresHuman},
		{"stall cap without progress", []map[string]bool{{"one": true}}, 8, 2, 3, models.TaskStateFailedRequiresHuman},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			script := fakeagent.Script(report(map[string]any{"summary": "done"}))
			w, store, task, project, profile, _, _ := acpFixture(t, script, false)
			task.SuccessCriteria = []string{"one", "two", "three", "four"}
			for _, c := range task.SuccessCriteria {
				task.GoldenCriteria = append(task.GoldenCriteria, models.GoldenCriterion{Criterion: c, CheckCommand: c})
			}
			sb := &goldenTestSandbox{results: tc.results}
			w.sandbox, w.sink = sb, &storeEventSink{store: store}
			w.goldenCycleCap, w.goldenStallCap = tc.cycleCap, tc.stallCap
			for i := 0; i < tc.attempts; i++ {
				w.dispatchProcess(ctx, task, project, profile)
				got := mustGetTask(t, store, task.ID)
				if i+1 == tc.attempts {
					if got.State != tc.want {
						t.Fatalf("state = %s, want %s", got.State, tc.want)
					}
					break
				}
				if got.State != models.TaskStateReady {
					t.Fatalf("cycle %d state = %s, want READY", i+1, got.State)
				}
				running, err := store.MarkTaskRunning(ctx, got.ID, got.UpdatedAt, 1)
				if err != nil {
					t.Fatal(err)
				}
				task.BaseEntity = running.BaseEntity
				task.State = running.State
			}
			if sb.checks != tc.attempts*4 {
				t.Fatalf("checks = %d, want %d", sb.checks, tc.attempts*4)
			}
		})
	}
}
