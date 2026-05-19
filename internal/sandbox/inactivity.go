package sandbox

import (
	"io"
	"sync"
	"time"
)

type inactivityReader struct {
	reader    io.Reader
	limit     time.Duration
	onTimeout func()
	timer     *time.Timer
	mu        sync.Mutex
}

func newInactivityReader(reader io.Reader, limit time.Duration, onTimeout func()) *inactivityReader {
	return &inactivityReader{
		reader:    reader,
		limit:     limit,
		onTimeout: onTimeout,
	}
}

func (r *inactivityReader) Read(p []byte) (int, error) {
	r.ensureTimer()
	n, err := r.reader.Read(p)
	if n > 0 {
		r.reset()
	}
	if err == io.EOF {
		r.stop()
	}
	return n, err
}

func (r *inactivityReader) ensureTimer() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timer != nil {
		return
	}
	r.timer = time.AfterFunc(r.limit, r.onTimeout)
}

func (r *inactivityReader) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timer == nil {
		return
	}
	r.timer.Reset(r.limit)
}

func (r *inactivityReader) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timer == nil {
		return
	}
	r.timer.Stop()
}
