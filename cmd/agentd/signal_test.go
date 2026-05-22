package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

const signalChildEnv = "AGENTD_SIGNAL_CHILD"

// TestSignalNotifyContextCancelsOnInterrupt verifies signal.NotifyContext in a
// child process. We re-exec the test binary instead of signaling os.Getpid(),
// which would deliver SIGINT to the go test runner and cause flaky CI failures.
func TestSignalNotifyContextCancelsOnInterrupt(t *testing.T) {
	if os.Getenv(signalChildEnv) == "1" {
		testSignalNotifyContextChild(t)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestSignalNotifyContextCancelsOnInterrupt$", "-test.count=1")
	cmd.Env = append(os.Environ(), signalChildEnv+"=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start child: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("Signal child: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil && !childExitedAfterInterrupt(err) {
			t.Fatalf("child exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child did not exit after interrupt")
	}
}

func childExitedAfterInterrupt(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == syscall.SIGINT
}

func testSignalNotifyContextChild(t *testing.T) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		os.Exit(0)
	case <-time.After(5 * time.Second):
		t.Fatal("context was not cancelled by interrupt")
	}
}
