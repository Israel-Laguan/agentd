package worker

import "strings"

// extractJSONCandidates returns candidate JSON payloads from raw model output:
// the trimmed whole output first, then the contents of ```json and ```
// markdown fences in document order. Callers attempt to decode each candidate
// into their result type and use the first one that decodes (and validates).
func extractJSONCandidates(output string) []string {
	trimmed := strings.TrimSpace(output)
	candidates := []string{trimmed}
	search := trimmed
	for {
		idx := strings.Index(search, "```")
		if idx == -1 {
			break
		}
		fence := "```"
		if strings.HasPrefix(search[idx:], "```json") {
			fence = "```json"
		}
		start := idx + len(fence)
		end := indexClosingFence(search[start:])
		if end == -1 {
			break
		}
		candidates = append(candidates, strings.TrimSpace(search[start:start+end]))
		search = search[start+end+len("```"):]
	}
	return candidates
}

// indexClosingFence returns the offset of the closing ``` fence within s, or
// -1 when no valid closer exists. A closer only counts at a markdown fence
// line boundary: after the fence only horizontal whitespace may follow before
// a newline or the end of the string. This keeps a literal ``` embedded in a
// JSON string value (followed by more payload on the same line) from
// truncating the candidate.
func indexClosingFence(s string) int {
	for off := 0; ; {
		idx := strings.Index(s[off:], "```")
		if idx == -1 {
			return -1
		}
		rest := s[off+idx+len("```"):]
		rest = strings.TrimLeft(rest, " \t\r")
		if rest == "" || strings.HasPrefix(rest, "\n") {
			return off + idx
		}
		off += idx + len("```")
	}
}
