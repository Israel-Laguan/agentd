package worker

import (
	"strings"
	"time"
)

// DetectContradictions scans compressed-zone summaries for facts that are
// directly contradicted by a tool result. It uses exact-match and key-based
// heuristics; semantic similarity is out of scope.
//
// A contradiction is detected when a tool result contains a negation pattern
// for a fact ("not <fact>", "no longer <fact>") or when the tool result
// contains an explicit key=value pair whose value differs from an
// established fact with the same key.
func DetectContradictions(summaries []TurnSummary, toolOutput string) []CorrectionRecord {
	if toolOutput == "" {
		return nil
	}
	lower := strings.ToLower(strings.TrimSpace(toolOutput))
	toolKV := extractKeyValues(lower)

	var corrections []CorrectionRecord
	now := time.Now()

	for _, s := range summaries {
		for _, fact := range s.FactsEstablished {
			if fact == "" {
				continue
			}
			lowerFact := strings.ToLower(fact)

			if detectNegation(lower, lowerFact) {
				corrections = append(corrections, CorrectionRecord{
					Contradiction: fact,
					CorrectFact:   extractCorrectFact(toolOutput, fact),
					Source:        CorrectionSourceTool,
					Timestamp:     now,
				})
				continue
			}

			if rec, ok := detectBooleanFlip(fact, lowerFact, lower, now); ok {
				corrections = append(corrections, rec)
				continue
			}

			if rec, ok := detectChangedPattern(fact, lowerFact, lower, now); ok {
				corrections = append(corrections, rec)
				continue
			}

			if rec, ok := detectKeyValueConflict(fact, lowerFact, toolKV, now); ok {
				corrections = append(corrections, rec)
				continue
			}

			if rec, ok := detectProseValueChange(fact, lowerFact, lower, now); ok {
				corrections = append(corrections, rec)
			}
		}
	}
	return corrections
}

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
			subject := strings.TrimSpace(lowerFact[:idx] + lowerFact[idx+len(flip.from):])
			if subject != "" && strings.Contains(lowerOutput, subject) && strings.Contains(lowerOutput, flip.to) {
				return CorrectionRecord{
					Contradiction: originalFact,
					CorrectFact:   formatBooleanFact(subject, flip.to),
					Source:        CorrectionSourceTool,
					Timestamp:     now,
				}, true
			}
		}
	}
	return CorrectionRecord{}, false
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
	r := text[idx]
	return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_')
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
		if idx := strings.Index(lowerOutput, p); idx > 0 {
			subject := strings.TrimSpace(lowerOutput[:idx])
			// If the output says "Port changed to 8080", check if fact contains "port"
			if subject != "" && strings.Contains(lowerFact, subject) {
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
		}
	}
	return CorrectionRecord{}, false
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
			// Check if output contains the subject and the same verb but different value
			if strings.Contains(lowerOutput, subject+v) {
				valIdx := strings.Index(lowerOutput, subject+v) + len(subject+v)
				lineEnd := strings.Index(lowerOutput[valIdx:], "\n")
				if lineEnd == -1 {
					lineEnd = len(lowerOutput[valIdx:])
				}
				newVal := strings.TrimSpace(lowerOutput[valIdx : valIdx+lineEnd])
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

// detectNegation checks whether the tool output contains a negation of the
// given fact. It looks for patterns like "not <fact>", "no longer <fact>",
// and also handles inline negation where "X is Y" becomes "X is not Y".
func detectNegation(lowerOutput, lowerFact string) bool {
	prefixed := []string{
		"not " + lowerFact,
		"no longer " + lowerFact,
		"isn't " + lowerFact,
		"is not " + lowerFact,
	}
	for _, neg := range prefixed {
		if strings.Contains(lowerOutput, neg) {
			return true
		}
	}

	// Handle inline negation: "X is Y" → check for "X is not Y" in output.
	for _, verb := range []string{" is ", " are ", " was ", " were "} {
		if idx := strings.Index(lowerFact, verb); idx >= 0 {
			subject := lowerFact[:idx]
			rest := lowerFact[idx+len(verb):]
			negated := subject + verb + "not " + rest
			if strings.Contains(lowerOutput, negated) {
				return true
			}
		}
	}
	return false
}

// detectKeyValueConflict checks whether the tool output contains a key=value
// pair whose value differs from an established fact with the same key.
func detectKeyValueConflict(
	originalFact, lowerFact string,
	toolKV map[string]string,
	now time.Time,
) (CorrectionRecord, bool) {
	factKV := extractKeyValues(lowerFact)
	for key, factVal := range factKV {
		toolVal, exists := toolKV[key]
		if !exists {
			continue
		}
		if toolVal != factVal {
			return CorrectionRecord{
				Contradiction: originalFact,
				CorrectFact:   key + "=" + toolVal,
				Source:        CorrectionSourceTool,
				Timestamp:     now,
			}, true
		}
	}
	return CorrectionRecord{}, false
}

// extractKeyValues parses simple key=value or key: value pairs from a
// lowercased text block and returns them as a map.
func extractKeyValues(text string) map[string]string {
	kv := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if sep := findSeparator(line); sep != "" {
			parts := strings.SplitN(line, sep, 2)
			if len(parts) == 2 {
				k := strings.TrimSpace(parts[0])
				v := strings.TrimSpace(parts[1])
				if k != "" && v != "" {
					kv[k] = v
				}
			}
		}
	}
	return kv
}

// findSeparator returns the first key-value separator found in the line,
// or an empty string if none is found. It checks "=" before ": " to
// prefer the more explicit separator.
func findSeparator(line string) string {
	if strings.Contains(line, "=") {
		return "="
	}
	if strings.Contains(line, ": ") {
		return ": "
	}
	return ""
}

// extractCorrectFact attempts to pull the most relevant correction text from
// the tool output when a negation-based contradiction is detected. It returns
// the first line that contains the negation, trimmed for readability.
func extractCorrectFact(toolOutput, contradictedFact string) string {
	lower := strings.ToLower(contradictedFact)
	for _, line := range strings.Split(toolOutput, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lowerLine := strings.ToLower(trimmed)
		if containsNegationOf(lowerLine, lower) {
			return trimmed
		}
	}
	return toolOutput
}

// containsNegationOf returns true when the line contains any negation form
// of the given lowercased fact string.
func containsNegationOf(lowerLine, lowerFact string) bool {
	if strings.Contains(lowerLine, "not "+lowerFact) ||
		strings.Contains(lowerLine, "no longer "+lowerFact) ||
		strings.Contains(lowerLine, "isn't "+lowerFact) ||
		strings.Contains(lowerLine, "is not "+lowerFact) {
		return true
	}
	for _, verb := range []string{" is ", " are ", " was ", " were "} {
		if idx := strings.Index(lowerFact, verb); idx >= 0 {
			subject := lowerFact[:idx]
			rest := lowerFact[idx+len(verb):]
			if strings.Contains(lowerLine, subject+verb+"not "+rest) {
				return true
			}
		}
	}
	return false
}
