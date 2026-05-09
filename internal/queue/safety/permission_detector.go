package safety

import (
	"regexp"
	"strings"
)

type PermissionDetection struct {
	Detected bool
	Pattern  string
}

var permissionPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{name: "Permission denied", re: regexp.MustCompile(`(?i)\bpermission denied\b`)},
	{name: "operation not permitted", re: regexp.MustCompile(`(?i)\boperation not permitted\b`)},
	{name: "must be root", re: regexp.MustCompile(`(?i)\bmust be root\b`)},
	{name: "requires root", re: regexp.MustCompile(`(?i)\brequires root\b`)},
	{name: "sudo:", re: regexp.MustCompile(`(?i)\bsudo:`)},
	{name: "sudo command blocked", re: regexp.MustCompile(`(?i)\bsudo command blocked\b`)},
	{name: "EACCES", re: regexp.MustCompile(`(?i)\bEACCES\b`)},
	{name: "cannot open.*permission", re: regexp.MustCompile(`(?i)\bcannot open\b.*\bpermission\b`)},
}

func DetectPermission(stdout, stderr string) PermissionDetection {
	output := strings.TrimSpace(stdout + "\n" + stderr)
	if output == "" {
		return PermissionDetection{}
	}
	for _, pattern := range permissionPatterns {
		if pattern.re.MatchString(output) {
			return PermissionDetection{Detected: true, Pattern: pattern.name}
		}
	}
	return PermissionDetection{}
}
