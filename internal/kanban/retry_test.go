package kanban

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type fakeBusyError struct{}

func (e *fakeBusyError) Error() string { return "database is locked (SQLITE_BUSY)" }
func (e *fakeBusyError) Code() int     { return sqliteBusy }

func newBusyError() error {
	return fmt.Errorf("exec failed: %w", &fakeBusyError{})
}

func TestRetryOnBusy_SucceedsAfterTransientBusy(t *testing.T) {
	attempts := 0
	result, err := retryOnBusy(context.Background(), func(_ context.Context) (string, error) {
		attempts++
		if attempts < 3 {
			return "", newBusyError()
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("retryOnBusy() error = %v", err)
	}
	if result != "ok" {
		t.Fatalf("result = %q, want %q", result, "ok")
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestRetryOnBusy_NonBusyErrorFailsImmediately(t *testing.T) {
	sentinel := errors.New("not a busy error")
	attempts := 0
	_, err := retryOnBusy(context.Background(), func(_ context.Context) (int, error) {
		attempts++
		return 0, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("retryOnBusy() error = %v, want sentinel", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry on non-busy)", attempts)
	}
}

func TestRetryOnBusy_ExhaustsRetries(t *testing.T) {
	attempts := 0
	_, err := retryOnBusy(context.Background(), func(_ context.Context) (int, error) {
		attempts++
		return 0, newBusyError()
	})
	if err == nil {
		t.Fatal("retryOnBusy() should return error after exhausting retries")
	}
	if attempts != maxBusyRetries {
		t.Fatalf("attempts = %d, want %d", attempts, maxBusyRetries)
	}
}

func TestRetryOnBusy_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	attempts := 0
	_, err := retryOnBusy(ctx, func(_ context.Context) (int, error) {
		attempts++
		return 0, newBusyError()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retryOnBusy() error = %v, want context.Canceled", err)
	}
}

func TestRetryOnBusyNoResult(t *testing.T) {
	attempts := 0
	err := retryOnBusyNoResult(context.Background(), func(_ context.Context) error {
		attempts++
		if attempts < 2 {
			return newBusyError()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryOnBusyNoResult() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}
