package worker

import "testing"

func TestToolFailureTracker_StreakAndReset(t *testing.T) {
	t.Parallel()
	tr := newToolFailureTracker(3)
	if failed, _ := tr.Record("bash", ToolStatusError); failed {
		t.Fatal("first error should not fail")
	}
	if failed, n := tr.Record("bash", ToolStatusError); failed || n != 2 {
		t.Fatalf("second error: failed=%v n=%d", failed, n)
	}
	if failed, _ := tr.Record("bash", ToolStatusSuccess); failed {
		t.Fatal("success should reset")
	}
	if failed, _ := tr.Record("bash", ToolStatusError); failed {
		t.Fatal("streak reset after success")
	}
	if failed, n := tr.Record("bash", ToolStatusError); failed || n != 2 {
		t.Fatalf("rebuild streak: failed=%v n=%d", failed, n)
	}
	if failed, _ := tr.Record("bash", ToolStatusError); !failed {
		t.Fatal("third consecutive error should fail")
	}
}

func TestToolFailureTracker_ResetClearsStreak(t *testing.T) {
	t.Parallel()
	tr := newToolFailureTracker(3)
	tr.Record("bash", ToolStatusError)
	tr.Record("bash", ToolStatusError)
	tr.reset()
	if failed, n := tr.Record("bash", ToolStatusError); failed || n != 1 {
		t.Fatalf("after reset: failed=%v n=%d, want streak 1", failed, n)
	}
}

func TestToolFailureTracker_FatalImmediate(t *testing.T) {
	t.Parallel()
	tr := newToolFailureTracker(3)
	if failed, n := tr.Record("bash", ToolStatusFatal); !failed || n != 1 {
		t.Fatalf("fatal: failed=%v n=%d", failed, n)
	}
}
