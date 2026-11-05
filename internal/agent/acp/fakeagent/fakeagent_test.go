package fakeagent

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestRunUsageOnlyStepIsSilent(t *testing.T) {
	a := &agent{}
	stop, err := a.run(context.Background(), "session", Step{Usage: &acp.Usage{TotalTokens: 1}})
	if stop != "" || err != nil {
		t.Fatalf("run = %q, %v; want a silent step", stop, err)
	}
}

func TestScriptFromPromptFindsScriptInContextPack(t *testing.T) {
	script := Script(Step{Usage: &acp.Usage{TotalTokens: 321}})
	pack := "a system message\n" + script + "\n\noutcome prompt"

	steps := scriptFromPrompt(pack)
	if len(steps) != 1 || steps[0].Usage == nil || steps[0].Usage.TotalTokens != 321 {
		t.Fatalf("steps = %+v, want the usage step from the context pack", steps)
	}
}

func TestScriptFromPromptRejectsBlankOrUnknownArrayLines(t *testing.T) {
	for _, line := range []string{"[]", "[{}]", `[{"unknown":true}]`} {
		if steps := scriptFromPrompt("a system message\n" + line); steps != nil {
			t.Fatalf("scriptFromPrompt(%q) = %+v, want no script from a stray array line", line, steps)
		}
	}
}
