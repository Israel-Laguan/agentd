package db

import (
	"context"
	"errors"
	"time"
)

const (
	sqliteBusy   = 5
	sqliteLocked = 6

	maxBusyRetries    = 6
	initialBackoff    = 5 * time.Millisecond
	backoffMultiplier = 2
)

type sqliteCodeError interface {
	Code() int
}

func isSQLiteBusy(err error) bool {
	var coded sqliteCodeError
	if errors.As(err, &coded) {
		code := coded.Code()
		return code == sqliteBusy || code == sqliteLocked
	}
	return false
}

// RetryOnBusy retries op when SQLite returns BUSY or LOCKED, using
// exponential back-off up to maxBusyRetries attempts.
func RetryOnBusy[T any](ctx context.Context, op func(context.Context) (T, error)) (T, error) {
	backoff := initialBackoff
	for attempt := 0; ; attempt++ {
		result, err := op(ctx)
		if err == nil || !isSQLiteBusy(err) || attempt >= maxBusyRetries-1 {
			return result, err
		}
		select {
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= backoffMultiplier
	}
}

// RetryOnBusyNoResult is a convenience wrapper around RetryOnBusy for
// operations that return only an error.
func RetryOnBusyNoResult(ctx context.Context, op func(context.Context) error) error {
	_, err := RetryOnBusy(ctx, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, op(ctx)
	})
	return err
}
