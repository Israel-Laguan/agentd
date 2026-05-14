package worker

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type booleanFlip struct {
	from string
	to   string
}

var booleanFlips = []booleanFlip{
	{from: "enabled", to: "disabled"},
	{from: "disabled", to: "enabled"},
	{from: "active", to: "inactive"},
	{from: "inactive", to: "active"},
	{from: "running", to: "stopped"},
	{from: "stopped", to: "running"},
	{from: "true", to: "false"},
	{from: "false", to: "true"},
	{from: "on", to: "off"},
	{from: "off", to: "on"},
	{from: "yes", to: "no"},
	{from: "no", to: "yes"},
}

func detectBooleanFlip(originalFact, lowerFact, lowerOutput string, now time.Time) (CorrectionRecord, bool) {
	for _, flip := range booleanFlips {
		if idx := lastTokenIndex(lowerFact, flip.from); idx >= 0 {
			subject := strings.Join(strings.Fields(lowerFact[:idx]+lowerFact[idx+len(flip.from):]), " ")
			if subject == "" {
				continue
			}
			outIdx := lastTokenIndex(lowerOutput, subject)
			if outIdx < 0 {
				continue
			}
			clause := boundedClause(lowerOutput[outIdx:])
			if lastTokenIndex(clause, flip.to) < 0 {
				continue
			}
			return CorrectionRecord{
				Contradiction: originalFact,
				CorrectFact:   formatBooleanFact(subject, flip.to),
				Source:        CorrectionSourceTool,
				Timestamp:     now,
			}, true
		}
	}
	return CorrectionRecord{}, false
}

func boundedClause(text string) string {
	end := len(text)
	if idx := strings.IndexAny(text, "\n.;,"); idx >= 0 {
		end = idx
	}
	for _, delimiter := range []string{" and ", " but ", " or "} {
		if idx := strings.Index(text, delimiter); idx >= 0 && idx < end {
			end = idx
		}
	}
	if end < len(text) {
		return text[:end]
	}
	return text
}

func lastTokenIndex(text, token string) int {
	for end := len(text); end > 0; {
		idx := strings.LastIndex(text[:end], token)
		if idx < 0 {
			return -1
		}
		if isTokenBoundary(text, idx-1) && isTokenBoundary(text, idx+len(token)) {
			return idx
		}
		end = idx
	}
	return -1
}

func isTokenBoundary(text string, idx int) bool {
	if idx < 0 || idx >= len(text) {
		return true
	}
	start := idx
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	r, _ := utf8.DecodeRuneInString(text[start:])
	if r == utf8.RuneError {
		return true
	}
	return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
}

func formatBooleanFact(subject, value string) string {
	for _, suffix := range []string{" is", " are", " was", " were"} {
		if strings.HasSuffix(subject, suffix) {
			return subject + " " + value
		}
	}
	return subject + " is " + value
}

func detectChangedPattern(originalFact, lowerFact, lowerOutput string, now time.Time) (CorrectionRecord, bool) {
	patterns := []string{" changed to ", " updated to ", " is now "}
	for _, p := range patterns {
		for searchStart := 0; searchStart < len(lowerOutput); {
			relIdx := strings.Index(lowerOutput[searchStart:], p)
			if relIdx < 0 {
				break
			}
			idx := searchStart + relIdx
			subject := strings.TrimSpace(clauseBefore(lowerOutput, idx))
			// If the output says "Port changed to 8080", check if fact contains "port".
			if subject != "" && lastTokenIndex(lowerFact, subject) >= 0 {
				newValue := trimChangedValue(lowerOutput[idx+len(p):])
				if newValue != "" {
					return CorrectionRecord{
						Contradiction: originalFact,
						CorrectFact:   subject + " is " + newValue,
						Source:        CorrectionSourceTool,
						Timestamp:     now,
					}, true
				}
			}
			searchStart = idx + len(p)
		}
	}
	return CorrectionRecord{}, false
}

func clauseBefore(text string, end int) string {
	start := 0
	for _, delimiter := range []string{"\n", ".", ";", ",", " and ", " but ", " or "} {
		if idx := strings.LastIndex(text[:end], delimiter); idx >= 0 && idx+len(delimiter) > start {
			start = idx + len(delimiter)
		}
	}
	return text[start:end]
}

func trimChangedValue(value string) string {
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\n', ',', ';':
			return strings.TrimSpace(value[:i])
		case '.':
			if i == len(value)-1 || value[i+1] == ' ' || value[i+1] == '\t' {
				return strings.TrimSpace(value[:i])
			}
		}
	}
	for _, delimiter := range []string{" and ", " but ", " or "} {
		if idx := strings.Index(value, delimiter); idx >= 0 {
			return strings.TrimSpace(value[:idx])
		}
	}
	return strings.TrimSpace(value)
}

func detectProseValueChange(originalFact, lowerFact, lowerOutput string, now time.Time) (CorrectionRecord, bool) {
	verbs := []string{" is ", " runs on ", " uses ", " at "}
	for _, v := range verbs {
		if idx := strings.Index(lowerFact, v); idx > 0 {
			subject := strings.TrimSpace(lowerFact[:idx])
			if subject == "" {
				continue
			}
			phrase := subject + strings.TrimRight(v, " ")
			matchIdx := lastTokenIndex(lowerOutput, phrase)
			if matchIdx >= 0 {
				valIdx := matchIdx + len(phrase)
				lineEnd := strings.Index(lowerOutput[valIdx:], "\n")
				if lineEnd == -1 {
					lineEnd = len(lowerOutput[valIdx:])
				}
				newVal := trimChangedValue(strings.TrimSpace(lowerOutput[valIdx : valIdx+lineEnd]))
				oldVal := strings.TrimSpace(lowerFact[idx+len(v):])

				if newVal != "" && newVal != oldVal {
					return CorrectionRecord{
						Contradiction: originalFact,
						CorrectFact:   subject + v + newVal,
						Source:        CorrectionSourceTool,
						Timestamp:     now,
					}, true
				}
			}
		}
	}
	return CorrectionRecord{}, false
}
