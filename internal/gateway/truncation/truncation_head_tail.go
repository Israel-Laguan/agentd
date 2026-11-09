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
	for len(marker) < budget {
		head, tail := headAndTail(input, budget-len(marker), ratio)
		next := fmt.Sprintf("%s(%d bytes omitted)", truncationMarker, len(input)-len(head)-len(tail))
		if len(next) == len(marker) {
			return head + next + tail
		}
		marker = next
	}
	// Too small for the count: the bare marker still shows the cut.
	if len(truncationMarker) < budget {
		head, tail := headAndTail(input, budget-len(truncationMarker), ratio)
		return head + truncationMarker + tail
	}
	return utf8SafePrefix(input, budget)
}

func headAndTail(input string, remaining int, ratio float64) (string, string) {
	headSize := int(float64(remaining) * ratio)
	return utf8SafePrefix(input, headSize), utf8SafeSuffix(input, remaining-headSize)
}
