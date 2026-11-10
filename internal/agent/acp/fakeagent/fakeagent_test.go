package fakeagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestEchoStepSkipsScriptLines(t *testing.T) {
	script := Script(Step{Echo: "CANARY"})
	step := echoStep("CANARY", "pref: CANARY 1\n"+script+"\nother")
	summary := step.Tool.Args.(map[string]any)["summary"].(string)
	if !strings.HasPrefix(summary, "echo bytes=") || !strings.HasSuffix(summary, "matched=1: pref: CANARY 1") {
		t.Fatalf("summary = %q, want exactly the one non-script line", summary)
	}
}

func TestRunSleepStopsOnCancel(t *testing.T) {
	a := &agent{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stop, err := a.run(ctx, "session", Step{SleepMS: 60_000})
	if stop != acp.StopReasonCancelled || err != nil {
		t.Fatalf("run = %q, %v; want cancelled", stop, err)
	}
}

func TestEchoStepKeepsBracketPrefixedPreference(t *testing.T) {
	prompt := "[note] CANARY keep me\n" + Script(Step{Echo: "CANARY"})
	summary := echoStep("CANARY", prompt).Tool.Args.(map[string]any)["summary"].(string)
	if !strings.HasSuffix(summary, "matched=1: [note] CANARY keep me") {
		t.Fatalf("summary = %q, want the bracket-prefixed line kept and the script dropped", summary)
	}
}

// T-074: the readiness marker lands in the session's workspace, by base name.
func TestRunTouchWritesTheMarkerInTheWorkspace(t *testing.T) {
	dir := t.TempDir()
	a := &agent{cwd: map[acp.SessionId]string{"s": dir}}
	if stop, err := a.run(context.Background(), "s", Step{Touch: "../ready.marker"}); stop != "" || err != nil {
		t.Fatalf("run = %q, %v; want no stop and no error", stop, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ready.marker")); err != nil {
		t.Fatalf("marker not in the workspace: %v", err)
	}
}
