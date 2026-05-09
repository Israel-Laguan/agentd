package safety

import "testing"

func TestDetectPermission(t *testing.T) {
	tests := []struct {
		name    string
		stdout  string
		stderr  string
		want    bool
		pattern string
	}{
		{name: "permission denied", stderr: "mkdir: cannot create directory: Permission denied", want: true, pattern: "Permission denied"},
		{name: "operation not permitted", stderr: "chmod: operation not permitted", want: true, pattern: "operation not permitted"},
		{name: "must be root", stdout: "This command must be root to continue", want: true, pattern: "must be root"},
		{name: "requires root", stderr: "installer requires root privileges", want: true, pattern: "requires root"},
		{name: "sudo", stderr: "sudo: a password is required", want: true, pattern: "sudo:"},
		{name: "sudo blocked", stderr: "sudo command blocked", want: true, pattern: "sudo command blocked"},
		{name: "eacces", stderr: "open /var/log/app: EACCES", want: true, pattern: "EACCES"},
		{name: "cannot open permission", stderr: "cannot open /etc/hosts: permission problem", want: true, pattern: "cannot open.*permission"},
		{name: "clean output", stdout: "done", stderr: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectPermission(tt.stdout, tt.stderr)
			if got.Detected != tt.want {
				t.Fatalf("Detected = %v, want %v", got.Detected, tt.want)
			}
			if tt.pattern != "" && got.Pattern != tt.pattern {
				t.Fatalf("Pattern = %q, want %q", got.Pattern, tt.pattern)
			}
		})
	}
}
