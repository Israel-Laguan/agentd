package main

import (
	"context"
	"strings"
	"testing"
)

// jail-forward is reached through the root command, stays out of --help, and
// hands its arguments to egress.Forward.
func TestJailForwardIsHiddenAndRunsTheForwarder(t *testing.T) {
	cmd, _, err := newRootCommand().Find([]string{"jail-forward"})
	if err != nil || cmd.Name() != "jail-forward" {
		t.Fatalf("Find(jail-forward) = %v, %v; want the registered command", cmd, err)
	}
	if !cmd.Hidden {
		t.Fatal("jail-forward is listed in --help; want it hidden")
	}
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"agentd", "jail-forward"}, "requires at least 1 arg"},
		{[]string{"agentd", "jail-forward", "127.0.0.1:3128"}, "is not ADDR=SOCKET"},
	}
	for _, tt := range tests {
		err := execute(context.Background(), tt.args)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("execute(%q) = %v, want an error containing %q", tt.args, err, tt.want)
		}
	}
}
