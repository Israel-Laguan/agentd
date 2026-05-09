package recovery

import "testing"

func TestCanRecover(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		wantOK      bool
		wantCommand string
	}{
		{name: "apt get", command: "apt-get install foo", wantOK: true, wantCommand: "apt-get -y install foo"},
		{name: "already flagged", command: "apt-get -y install foo", wantOK: true, wantCommand: "apt-get -y install foo"},
		{name: "unknown command", command: "ssh example.com", wantOK: false},
		{name: "pip install", command: "pip install foo", wantOK: true, wantCommand: "PIP_NO_INPUT=1 pip install --no-input foo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, command := CanRecover(tt.command)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if command != tt.wantCommand {
				t.Fatalf("command = %q, want %q", command, tt.wantCommand)
			}
		})
	}
}
