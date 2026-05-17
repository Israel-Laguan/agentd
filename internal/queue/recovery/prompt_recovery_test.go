package recovery

import "testing"

func TestCanRecover(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		wantOK      bool
		wantCommand string
	}{
		{name: "empty", command: "", wantOK: false},
		{name: "whitespace", command: "   ", wantOK: false},
		{name: "apt-get", command: "apt-get install foo", wantOK: true, wantCommand: "apt-get -y install foo"},
		{name: "apt", command: "apt install foo", wantOK: true, wantCommand: "apt -y install foo"},
		{name: "yum", command: "yum install foo", wantOK: true, wantCommand: "yum -y install foo"},
		{name: "dnf", command: "dnf install foo", wantOK: true, wantCommand: "dnf -y install foo"},
		{name: "pacman", command: "pacman -S foo", wantOK: true, wantCommand: "pacman --noconfirm -S foo"},
		{name: "already flagged apt", command: "apt-get -y install foo", wantOK: true, wantCommand: "apt-get -y install foo"},
		{name: "pip install", command: "pip install foo", wantOK: true, wantCommand: "PIP_NO_INPUT=1 pip install --no-input foo"},
		{name: "pip3 install", command: "pip3 install foo", wantOK: true, wantCommand: "PIP_NO_INPUT=1 pip3 install --no-input foo"},
		{name: "pip already no-input", command: "pip install --no-input foo", wantOK: true, wantCommand: "PIP_NO_INPUT=1 pip install --no-input foo"},
		{name: "python pip", command: "python3 -m pip install foo", wantOK: true, wantCommand: "PIP_NO_INPUT=1 python3 -m pip install --no-input foo"},
		{name: "pip list", command: "pip list", wantOK: false},
		{name: "npm install", command: "npm install foo", wantOK: true, wantCommand: "npm --yes install foo"},
		{name: "npm ci", command: "npm ci", wantOK: false},
		{name: "brew install", command: "brew install foo", wantOK: true, wantCommand: "NONINTERACTIVE=1 brew install foo"},
		{name: "brew upgrade", command: "brew upgrade foo", wantOK: false},
		{name: "unknown command", command: "ssh example.com", wantOK: false},
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

func TestInsertToken_AppendsWhenAnchorMissing(t *testing.T) {
	got := insertToken("custom-cmd install", "missing", "-y")
	if got != "custom-cmd install -y" {
		t.Fatalf("insertToken() = %q", got)
	}
}
