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
