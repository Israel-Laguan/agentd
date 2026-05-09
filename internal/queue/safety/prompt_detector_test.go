package safety

import "testing"

func TestDetectPrompt(t *testing.T) {
	tests := []struct {
		name    string
		stdout  string
		stderr  string
		want    bool
		pattern string
	}{
		{name: "yes no prompt", stdout: "Install package? [y/N]", want: true, pattern: "[y/N]"},
		{name: "password prompt", stderr: "Password:", want: true, pattern: "password:"},
		{name: "are you sure prompt", stdout: "Are you sure you want to continue?", want: true, pattern: "Are you sure"},
		{name: "clean output", stdout: "done", stderr: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectPrompt(tt.stdout, tt.stderr)
			if got.Detected != tt.want {
				t.Fatalf("Detected = %v, want %v", got.Detected, tt.want)
			}
			if tt.pattern != "" && got.Pattern != tt.pattern {
				t.Fatalf("Pattern = %q, want %q", got.Pattern, tt.pattern)
			}
		})
	}
}
