package truncation

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
	if budget <= len(truncationMarker) {
		return input[:budget]
	}

	ratio := normalizeHeadRatio(s.HeadRatio)
	remaining := budget - len(truncationMarker)
	head := int(float64(remaining) * ratio)
	tail := remaining - head
	return input[:head] + truncationMarker + input[len(input)-tail:]
}
