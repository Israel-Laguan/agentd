package tools

import (
	"crypto/sha256"
	"encoding/hex"
)

// DefaultNoProgressRepeats is how many times one tool call (same name and
// arguments) may return the same result before the loop counts it as stuck.
const DefaultNoProgressRepeats = 3

// RepeatTracker counts identical tool calls that return identical results
// within one task run (B-081). A repeat whose result changed, such as a test
// run after an edit, is progress and is counted separately.
type RepeatTracker struct {
	threshold int
	seen      map[string]int
}

// NewRepeatTracker returns nil (never stuck) when threshold is not positive.
func NewRepeatTracker(threshold int) *RepeatTracker {
	if threshold <= 0 {
		return nil
	}
	return &RepeatTracker{threshold: threshold, seen: make(map[string]int)}
}

// Record counts one call and reports whether it reached the threshold.
func (t *RepeatTracker) Record(toolName, arguments, result string) (stuck bool, count int) {
	if t == nil {
		return false, 0
	}
	sum := sha256.Sum256([]byte(toolName + "\x00" + arguments + "\x00" + result))
	key := hex.EncodeToString(sum[:])
	t.seen[key]++
	n := t.seen[key]
	return n >= t.threshold, n
}
