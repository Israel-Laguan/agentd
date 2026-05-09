package recovery

import "strings"

// CanRecover checks whether a command that triggered an interactive prompt can
// be retried with a non-interactive flag. Recovery is intentionally limited to
// an allowlist of well-known package managers (apt, yum, dnf, pacman, pip, npm,
// brew) to avoid automatically approving destructive or unknown commands.
func CanRecover(command string) (bool, string) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return false, ""
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false, ""
	}

	switch fields[0] {
	case "apt-get", "apt", "yum", "dnf":
		return recoverWithFlag(trimmed, "-y")
	case "pacman":
		return recoverWithFlag(trimmed, "--noconfirm")
	case "pip", "pip3":
		return recoverPipInstall(trimmed, fields, fields[0]+" install")
	case "python", "python3":
		return recoverPythonPipInstall(trimmed, fields)
	case "npm":
		return recoverNpmInstall(trimmed, fields)
	case "brew":
		return recoverBrewInstall(trimmed, fields)
	default:
		return false, ""
	}
}

func recoverPipInstall(command string, fields []string, anchor string) (bool, string) {
	if len(fields) <= 1 || fields[1] != "install" {
		return false, ""
	}
	return enforcePipNoInput(command, fields, anchor)
}

func recoverPythonPipInstall(command string, fields []string) (bool, string) {
	if len(fields) <= 3 || fields[1] != "-m" || fields[2] != "pip" || fields[3] != "install" {
		return false, ""
	}
	return enforcePipNoInput(command, fields, fields[0]+" -m pip install")
}

func enforcePipNoInput(command string, fields []string, anchor string) (bool, string) {
	recovered := command
	if !hasToken(fields, "--no-input") {
		recovered = insertToken(command, anchor, "--no-input")
	}
	if strings.HasPrefix(recovered, "PIP_NO_INPUT=1 ") {
		return true, recovered
	}
	return true, "PIP_NO_INPUT=1 " + recovered
}

func recoverNpmInstall(command string, fields []string) (bool, string) {
	if len(fields) <= 1 || fields[1] != "install" {
		return false, ""
	}
	return recoverWithFlag(command, "--yes")
}

func recoverBrewInstall(command string, fields []string) (bool, string) {
	if len(fields) <= 1 || fields[1] != "install" {
		return false, ""
	}
	if strings.HasPrefix(command, "NONINTERACTIVE=1 ") {
		return true, command
	}
	return true, "NONINTERACTIVE=1 " + command
}

func recoverWithFlag(command, flag string) (bool, string) {
	fields := strings.Fields(command)
	if hasToken(fields, flag) {
		return true, command
	}
	return true, insertToken(command, fields[0], flag)
}

func hasToken(fields []string, token string) bool {
	for _, field := range fields {
		if field == token {
			return true
		}
	}
	return false
}

func insertToken(command, after, token string) string {
	idx := strings.Index(command, after)
	if idx < 0 {
		return command + " " + token
	}
	end := idx + len(after)
	return strings.TrimSpace(command[:end] + " " + token + command[end:])
}
