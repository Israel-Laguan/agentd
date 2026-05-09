//go:build !windows

package sandbox

import (
	"os/exec"
	"strings"
	"testing"
)

func TestConfigureProcessSetsProcessGroup(t *testing.T) {
	cmd := exec.Command("bash", "-c", "echo ok")
	configureProcess(cmd, ResourceLimits{})
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr = nil")
	}
	if !cmd.SysProcAttr.Setpgid {
		t.Fatal("Setpgid = false")
	}
}

func TestWithResourceLimitsWrapsCommandWithUlimit(t *testing.T) {
	command := withResourceLimits("echo ok", ResourceLimits{
		AddressSpaceBytes: 2048 * 1024,
		CPUSeconds:        10,
		OpenFiles:         128,
		Processes:         64,
	})
	for _, expected := range []string{"ulimit -Sv 2048", "ulimit -t 10", "ulimit -n 128", "ulimit -u 64", "echo ok"} {
		if !strings.Contains(command, expected) {
			t.Fatalf("command = %q, missing %q", command, expected)
		}
	}
}
