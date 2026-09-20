package worker

import "strings"

// extractJSONCandidates returns candidate JSON payloads from raw model output:
// the trimmed whole output first, then the contents of ```json and ```
// markdown fences in document order. Callers attempt to decode each candidate
// into their result type and use the first one that decodes (and validates).
func extractJSONCandidates(output string) []string {
	trimmed := strings.TrimSpace(output)
	candidates := []string{trimmed}
	for _, fence := range []string{"```json", "```"} {
		search := trimmed
		for {
			idx := strings.Index(search, fence)
			if idx == -1 {
				break
			}
			start := idx + len(fence)
			end := strings.Index(search[start:], "```")
			if end == -1 {
				break
			}
			candidates = append(candidates, strings.TrimSpace(search[start:start+end]))
			search = search[start+end+len("```"):]
		}
	}
	return candidates
}
