package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"agentd/internal/config"
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
	home := initHome(t)
	configPath := writeStartTestConfig(t)
	clearProviderKeys(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := execute(ctx, []string{
		"agentd", "--home", home, "--config", configPath, "start", "--skip-llm-warmup",
	})
	if err == nil {
		t.Fatal("execute() error = nil, want operational error from start without providers")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("start blocked until context deadline; expected immediate provider configuration error")
	}
	if !errors.Is(err, config.ErrNoLLMProviders) {
		t.Fatalf("error = %v, want %v", err, config.ErrNoLLMProviders)
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
