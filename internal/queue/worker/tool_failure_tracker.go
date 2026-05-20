package worker

// toolFailureTracker counts consecutive errors per tool name for LoopToolFailure.
type toolFailureTracker struct {
	threshold int
	streak    map[string]int
}

func newToolFailureTracker(threshold int) *toolFailureTracker {
	if threshold <= 0 {
		return nil
	}
	return &toolFailureTracker{
		threshold: threshold,
		streak:    make(map[string]int),
	}
}

func (t *toolFailureTracker) reset() {
	if t == nil {
		return
	}
	clear(t.streak)
}

func (t *toolFailureTracker) Record(toolName string, status ToolStatus) (failed bool, streak int) {
	if t == nil {
		return false, 0
	}
	switch status {
	case ToolStatusSuccess, ToolStatusVetoed:
		delete(t.streak, toolName)
		return false, 0
	case ToolStatusFatal:
		return true, 1
	case ToolStatusError, ToolStatusTimeout:
		t.streak[toolName]++
		n := t.streak[toolName]
		return n >= t.threshold, n
	default:
		return false, 0
	}
}
