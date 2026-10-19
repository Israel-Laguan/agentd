package queue

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

// guardTick runs one background-loop tick and reports whether it panicked. A returned error is logged under
// failMsg; a panic is recovered and logged with its stack, so one bad tick
// cannot kill the daemon and the loop carries on at its next interval.
func guardTick(failMsg string, tick func() error) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			slog.Error("daemon loop panic recovered", "loop", failMsg, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	logDaemonError(failMsg, tick())
	return false
}
