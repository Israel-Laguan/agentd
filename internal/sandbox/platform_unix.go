//go:build !windows

package sandbox

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

func configureProcess(cmd *exec.Cmd, _ ResourceLimits) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcessGroup(pid int, grace time.Duration) error {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	if grace > 0 {
		time.Sleep(grace)
	}
	return syscall.Kill(-pid, syscall.SIGKILL)
}

func withResourceLimits(command string, limits ResourceLimits) string {
	limitsScript := fmt.Sprintf(
		"(ulimit -Sv %d 2>/dev/null || true); (ulimit -t %d 2>/dev/null || true); (ulimit -n %d 2>/dev/null || true); (ulimit -u %d 2>/dev/null || true); %s",
		toKilobytes(limits.AddressSpaceBytes),
		limits.CPUSeconds,
		limits.OpenFiles,
		limits.Processes,
		command,
	)
	return limitsScript
}

func toKilobytes(bytes uint64) uint64 {
	if bytes == 0 {
		return 0
	}
	return bytes / 1024
}
