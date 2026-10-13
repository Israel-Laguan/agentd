package sandbox

import (
	"context"
	"strings"
	"testing"
)

func TestBashExecutorViolationEventNamesReasonAndCommand(t *testing.T) {
	sink := &recordingSink{}
	exec, workspace := testExecutor(t, sink)
	if _, err := exec.Execute(context.Background(), testPayload(workspace, "cat /etc/os-release")); err == nil {
		t.Fatal("Execute() error = nil, want sandbox violation")
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %#v, want one", sink.events)
	}
	got := sink.events[0].Payload
	for _, want := range []string{"absolute path escapes workspace", "/etc/os-release", "cat /etc/os-release"} {
		if !strings.Contains(got, want) {
			t.Fatalf("payload = %q, want substring %q", got, want)
		}
	}
}

func TestBashExecutorViolationEventTruncatesCommand(t *testing.T) {
	sink := &recordingSink{}
	exec, workspace := testExecutor(t, sink)
	command := "cat /etc/passwd " + strings.Repeat("x", 5000)
	if _, err := exec.Execute(context.Background(), testPayload(workspace, command)); err == nil {
		t.Fatal("Execute() error = nil, want sandbox violation")
	}
	got := sink.events[0].Payload
	if len(got) > 600 {
		t.Fatalf("payload length = %d, want truncated command (<= 600)", len(got))
	}
	if !strings.Contains(got, "...") {
		t.Fatalf("payload = %q, want truncation marker", got)
	}
}

func TestValidateAbsolutePathTokens_DevNull(t *testing.T) {
	tests := []struct {
		command string
		wantErr bool
	}{
		{"echo hi > /dev/null 2>&1", false},
		{"cmd 2> /dev/null", false},
		{"cat /dev/nullx", true},
		{"cat /dev/null/other", true},
		{"cat /dev/zero", true},
		{"cat /dev/stdin", true},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			err := validateAbsolutePathTokens(tt.command, "/workspace")
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateAbsolutePathTokens(%q) error = %v, wantErr %v", tt.command, err, tt.wantErr)
			}
		})
	}
}
