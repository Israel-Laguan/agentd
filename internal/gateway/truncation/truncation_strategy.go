package truncation

// TruncationStrategy is the extension point for context-size reduction.
type TruncationStrategy interface {
	Name() string
	Truncate(input string, budget int) string
}

const (
	// TruncationStrategyMiddleOut is the default middle-out strategy name.
	TruncationStrategyMiddleOut = "middle_out"
	// TruncationStrategyHeadTail names the head/tail strategy.
	TruncationStrategyHeadTail = "head_tail"
)

func normalizeHeadRatio(ratio float64) float64 {
	if ratio < 0 {
		return 0
	}
	if ratio > 1 {
		return 1
	}
	return ratio
}
