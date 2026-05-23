package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func TestReportCobraUsageError_UnknownCommand(t *testing.T) {
	err := execute(context.Background(), []string{"agentd", "bogus-cmd"})
	if err == nil {
		t.Fatal("execute() error = nil, want unknown command")
	}
	var buf bytes.Buffer
	if !reportCobraUsageError(err) {
		t.Fatalf("reportCobraUsageError() = false, want true for %v", err)
	}
	// reportCobraUsageError writes to os.Stderr; capture via pipe is heavy.
	// Verify error shape instead.
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("error = %v, want unknown command", err)
	}
	_ = buf
}

func TestReportCobraUsageError_OperationalError(t *testing.T) {
	err := execute(context.Background(), []string{"agentd", "start"})
	if err == nil {
		// start may fail for many reasons; we only need a non-cobra-parse error.
		t.Skip("start succeeded unexpectedly")
	}
	if reportCobraUsageError(err) {
		t.Fatalf("reportCobraUsageError() = true, want false for operational error %v", err)
	}
}

func TestReportCobraUsageError_UnknownFlag(t *testing.T) {
	err := execute(context.Background(), []string{"agentd", "status", "--not-a-real-flag"})
	if err == nil {
		t.Fatal("execute() error = nil, want flag error")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("error = %v, want unknown flag", err)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = old })
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	_ = w.Close()
	return <-done
}

func TestReportCobraUsageError_PrintsUsage(t *testing.T) {
	err := execute(context.Background(), []string{"agentd", "bogus-cmd"})
	if err == nil {
		t.Fatal("execute() error = nil")
	}
	out := captureStderr(t, func() {
		if !reportCobraUsageError(err) {
			t.Fatal("reportCobraUsageError() = false")
		}
	})
	if !strings.Contains(out, "unknown command") {
		t.Fatalf("stderr = %q, want unknown command message", out)
	}
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("stderr = %q, want Usage section", out)
	}
}
