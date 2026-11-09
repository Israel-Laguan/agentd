package truncation

import "fmt"

// HeadTailStrategy keeps a configurable amount from the start and end.
type HeadTailStrategy struct {
	HeadRatio float64
}

// Name returns the strategy identifier.
func (s HeadTailStrategy) Name() string {
	return TruncationStrategyHeadTail
}

// Truncate applies head/tail cutting with a marker in the middle.
func (s HeadTailStrategy) Truncate(input string, budget int) string {
	if budget <= 0 || len(input) <= budget {
		return input
	}
	ratio := normalizeHeadRatio(s.HeadRatio)
	marker := fmt.Sprintf("%s(%d bytes omitted)", truncationMarker, len(input))
	for {
		remaining := budget - len(marker)
		if remaining <= 0 {
			return utf8SafePrefix(input, budget)
		}
		headSize := int(float64(remaining) * ratio)
		head := utf8SafePrefix(input, headSize)
		tail := utf8SafeSuffix(input, remaining-headSize)
		next := fmt.Sprintf("%s(%d bytes omitted)", truncationMarker, len(input)-len(head)-len(tail))
		if len(next) == len(marker) {
			return head + next + tail
		}
		marker = next
	}
}
