package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	signalChildEnv       = "AGENTD_SIGNAL_CHILD"
	signalChildReadyLine = "signal-child-ready"
)

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
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("StderrPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start child: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	ready := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), signalChildReadyLine) {
				close(ready)
				return
			}
		}
	}()

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("child did not signal readiness")
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("Signal child: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child did not exit after interrupt")
	}
}

func testSignalNotifyContextChild(t *testing.T) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintln(os.Stderr, signalChildReadyLine)

	select {
	case <-ctx.Done():
		os.Exit(0)
	case <-time.After(5 * time.Second):
		t.Fatal("context was not cancelled by interrupt")
	}
}
