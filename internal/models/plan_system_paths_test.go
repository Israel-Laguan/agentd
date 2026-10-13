package models

import (
	"strings"
	"testing"
)

func TestDraftPlanValidate_RejectsSystemPathsInTasks(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		desc    string
		wantErr string
	}{
		{"cat os-release", "Gather OS information", "Run uname -a, hostnamectl, and cat /etc/os-release.", "/etc/os-release"},
		{"proc path in parens", "CPU", "Read the CPU model (/proc/cpuinfo) and save it.", "/proc/cpuinfo"},
		{"sys path", "DMI", "Print /sys/class/dmi/id/product_name", "/sys/class/dmi/id/product_name"},
		{"path in title", "Read /etc/hostname", "Save it", "/etc/hostname"},
		{"bare etc dir", "List config", "ls /etc", "/etc"},
		{"commands only", "Gather OS", "Run uname -a and hostnamectl and save to os.txt", ""},
		{"http route is not a system path", "API", "Expose GET /api/users and /healthz", ""},
		{"dev null is allowed", "Quiet", "Run lscpu 2>/dev/null > cpu.txt", ""},
		{"relative path", "Files", "Write notes to docs/etc/readme.md", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := &DraftPlan{ProjectName: "p", Tasks: []DraftTask{{Title: tt.title, Description: tt.desc}}}
			err := plan.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) || !strings.Contains(err.Error(), "task 1") {
				t.Fatalf("Validate() error = %v, want task 1 error naming %q", err, tt.wantErr)
			}
		})
	}
}
