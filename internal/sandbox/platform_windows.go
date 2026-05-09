//go:build windows

package sandbox

import (
	"os/exec"
	"strconv"
	"time"
)

func configureProcess(_ *exec.Cmd, _ ResourceLimits) {}

func terminateProcessGroup(pid int, _ time.Duration) error {
	return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
}

func withResourceLimits(command string, _ ResourceLimits) string {
	return command
}
