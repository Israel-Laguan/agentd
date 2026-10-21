package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// B-084: with no stdin (a script, CI, `< /dev/null`) the approval prompt read
// EOF and the CLI printed a generic failure whose only cause was "EOF".
func TestApproved_EmptyStdinNamesTheMissingTerminal(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&bytes.Buffer{})

	ok, err := approved(cmd)
	if err == nil || ok {
		t.Fatalf("approved() = %v, %v; want an error and no approval", ok, err)
	}
	summary, hint := describeCommandError(err)
	if !strings.Contains(summary, "nothing was created") {
		t.Errorf("summary = %q; want it to say nothing was created", summary)
	}
	if !strings.Contains(hint, "--yes") {
		t.Errorf("hint = %q; want it to name --yes", hint)
	}
}

func TestApprove_YesSkipsThePrompt(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(""))
	var out bytes.Buffer
	cmd.SetOut(&out)

	ok, err := (&askOptions{yes: true}).approve(cmd)
	if err != nil || !ok {
		t.Fatalf("approve() = %v, %v; want approved with no error", ok, err)
	}
	if strings.Contains(out.String(), "Do you approve") {
		t.Errorf("output = %q; want no prompt with --yes", out.String())
	}
}
