package worker

// SetLoopResultRecorder installs a test hook invoked when processAgentic returns a typed result.
func (w *Worker) SetLoopResultRecorder(fn func(LoopResult)) {
	w.loopResultRecorder = fn
}
