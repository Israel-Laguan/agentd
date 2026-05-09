package safety

import (
	"regexp"
	"strings"
)

type PromptDetection struct {
	Detected bool
	Pattern  string
}

var promptPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{name: "[y/N]", re: regexp.MustCompile(`(?i)\[[yn]/[yn]\]`)},
	{name: "[yes/no]", re: regexp.MustCompile(`(?i)\[(yes/no|no/yes)\]`)},
	{name: "Are you sure", re: regexp.MustCompile(`(?i)\bare you sure\b`)},
	{name: "Continue?", re: regexp.MustCompile(`(?i)\bcontinue\?`)},
	{name: "Proceed?", re: regexp.MustCompile(`(?i)\bproceed\?`)},
	{name: "Do you want to continue", re: regexp.MustCompile(`(?i)\bdo you want to continue\b`)},
	{name: "Press ENTER", re: regexp.MustCompile(`(?i)\bpress enter\b`)},
	{name: "Press any key", re: regexp.MustCompile(`(?i)\bpress any key\b`)},
	{name: "password:", re: regexp.MustCompile(`(?i)\bpassword:`)},
	{name: "passphrase:", re: regexp.MustCompile(`(?i)\bpassphrase:`)},
	{name: "enter input:", re: regexp.MustCompile(`(?i)\benter [^:\n]{1,80}:`)},
}

func DetectPrompt(stdout, stderr string) PromptDetection {
	output := strings.TrimSpace(stdout + "\n" + stderr)
	if output == "" {
		return PromptDetection{}
	}
	for _, pattern := range promptPatterns {
		if pattern.re.MatchString(output) {
			return PromptDetection{Detected: true, Pattern: pattern.name}
		}
	}
	return PromptDetection{}
}
