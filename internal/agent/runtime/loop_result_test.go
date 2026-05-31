package runtime

import "testing"

func TestLoopStatus_String(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status LoopStatus
		want   string
	}{
		{LoopSuccessfulCompletion, "successful_completion"},
		{LoopBudgetExhausted, "budget_exhausted"},
		{LoopTurnLimitExceeded, "turn_limit_exceeded"},
		{LoopToolFailure, "tool_failure"},
	}
	for _, tc := range cases {
		if got := tc.status.String(); got != tc.want {
			t.Errorf("LoopStatus(%d).String() = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestLoopResult_IsTerminalSuccess(t *testing.T) {
	t.Parallel()
	ok := LoopResult{Status: LoopSuccessfulCompletion}
	if !ok.IsTerminalSuccess() {
		t.Fatal("expected successful completion to be terminal success")
	}
	for _, status := range []LoopStatus{LoopBudgetExhausted, LoopTurnLimitExceeded, LoopToolFailure} {
		r := LoopResult{Status: status}
		if r.IsTerminalSuccess() {
			t.Fatalf("status %s should not be terminal success", status)
		}
	}
}
