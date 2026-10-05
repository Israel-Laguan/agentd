package sandbox

import (
	"io"
	"sync"
	"time"
)

// activityTracker is one inactivity timer shared by every stream of a command:
// output on any stream counts as activity, and the timer stops only once all
// of its streams have ended.
type activityTracker struct {
	limit     time.Duration
	onTimeout func()
	timer     *time.Timer
	open      int
	mu        sync.Mutex
}

func newActivityTracker(limit time.Duration, onTimeout func()) *activityTracker {
	return &activityTracker{
		limit:     limit,
		onTimeout: onTimeout,
	}
}

// watch returns a reader whose reads feed the tracker.
func (t *activityTracker) watch(reader io.Reader) *inactivityReader {
	t.mu.Lock()
	t.open++
	t.mu.Unlock()
	return &inactivityReader{
		reader:          reader,
		activityTracker: t,
	}
}

func (t *activityTracker) ensureTimer() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timer != nil {
		return
	}
	t.timer = time.AfterFunc(t.limit, t.onTimeout)
}

func (t *activityTracker) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timer == nil {
		return
	}
	t.timer.Reset(t.limit)
}

func (t *activityTracker) streamEnded() {
	t.mu.Lock()
	t.open--
	last := t.open <= 0
	t.mu.Unlock()
	if last {
		t.stop()
	}
}

func (t *activityTracker) stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timer == nil {
		return
	}
	t.timer.Stop()
}

type inactivityReader struct {
	reader io.Reader
	*activityTracker
	ended sync.Once
}

func (r *inactivityReader) Read(p []byte) (int, error) {
	r.ensureTimer()
	n, err := r.reader.Read(p)
	if n > 0 {
		r.reset()
	}
	if err == io.EOF {
		r.ended.Do(r.streamEnded)
	}
	return n, err
}
