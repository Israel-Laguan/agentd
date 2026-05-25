package providers

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestOptionDurationInvalidString(t *testing.T) {
	t.Parallel()

	const def = 4 * time.Second
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))
	old := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(old) })

	got := optionDuration(map[string]any{"poll_interval": "not-a-duration"}, "poll_interval", def)
	if got != def {
		t.Fatalf("optionDuration() = %v, want default %v", got, def)
	}
	logOut := buf.String()
	if !strings.Contains(logOut, "invalid duration") {
		t.Errorf("expected invalid duration error log; got %q", logOut)
	}
	if !strings.Contains(logOut, "poll_interval") {
		t.Errorf("expected log to name key poll_interval; got %q", logOut)
	}
}
