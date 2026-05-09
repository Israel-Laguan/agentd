package sandbox

import (
	"regexp"
	"strings"
)

const redactedToken = "[REDACTED]"

type Scrubber interface {
	Scrub(value string) string
}

type regexScrubber struct {
	patterns []*regexp.Regexp
}

func NewScrubber(customPatterns []string) Scrubber {
	patterns := make([]*regexp.Regexp, 0, len(defaultScrubPatterns)+len(customPatterns))
	for _, pattern := range defaultScrubPatterns {
		patterns = append(patterns, regexp.MustCompile(pattern))
	}
	for _, pattern := range customPatterns {
		trimmed := strings.TrimSpace(pattern)
		if trimmed == "" {
			continue
		}
		compiled, err := regexp.Compile(trimmed)
		if err != nil {
			continue
		}
		patterns = append(patterns, compiled)
	}
	return regexScrubber{patterns: patterns}
}

func (s regexScrubber) Scrub(value string) string {
	scrubbed := value
	for _, pattern := range s.patterns {
		scrubbed = pattern.ReplaceAllString(scrubbed, redactedToken)
	}
	return scrubbed
}

var defaultScrubPatterns = []string{
	`sk-[A-Za-z0-9]{20,}`,
	`ghp_[A-Za-z0-9]{30,}`,
	`AKIA[0-9A-Z]{16}`,
	`xox[abp]-[A-Za-z0-9-]+`,
	`Bearer\s+[A-Za-z0-9._\-]+`,
	`(?i)(api[_-]?key|secret|token|password)\s*[=:]\s*\S+`,
}
