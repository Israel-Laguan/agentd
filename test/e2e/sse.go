//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SSEEvent represents a server-sent event from /api/v1/sse.
type SSEEvent struct {
	Type string          // "task-started", "task-claimed", "task-completed", etc.
	Data json.RawMessage // Raw JSON data
}

// SSEReader reads from the /api/v1/events/stream endpoint.
type SSEReader struct {
	resp    *http.Response
	scanner *bufio.Scanner
	cancel  context.CancelFunc
}

// NewSSEReader opens the SSE endpoint, optionally scoped to one project's
// events, and returns a reader. The caller should call Close() when done.
func NewSSEReader(ctx context.Context, client *APIClient, projectID string) (*SSEReader, error) {
	readCtx, cancel := context.WithCancel(ctx)
	url := client.baseURL + "/api/v1/events/stream"
	if projectID != "" {
		url += "?project_id=" + projectID
	}
	req, err := http.NewRequestWithContext(readCtx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, err
	}

	resp, err := client.client.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("SSE endpoint returned %d", resp.StatusCode)
	}

	return &SSEReader{
		resp:    resp,
		scanner: bufio.NewScanner(resp.Body),
		cancel:  cancel,
	}, nil
}

// NextEvent reads the next SSE event with a timeout.
func (r *SSEReader) NextEvent(ctx context.Context, timeout time.Duration) (*SSEEvent, error) {
	type result struct {
		event *SSEEvent
		err   error
	}
	ch := make(chan result, 1)

	go func() {
		event, err := r.nextEventSync()
		ch <- result{event, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.event, res.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("SSE read timeout")
	}
}

func (r *SSEReader) nextEventSync() (*SSEEvent, error) {
	// Skip empty lines and comments.
	for r.scanner.Scan() {
		line := r.scanner.Text()
		if strings.HasPrefix(line, ":") || line == "" {
			continue
		}

		// Parse "event: <type>" or "data: <json>".
		if strings.HasPrefix(line, "event: ") {
			eventType := strings.TrimPrefix(line, "event: ")
			// Expect a data line next.
			if !r.scanner.Scan() {
				return nil, io.EOF
			}
			dataLine := r.scanner.Text()
			if !strings.HasPrefix(dataLine, "data: ") {
				continue
			}
			dataStr := strings.TrimPrefix(dataLine, "data: ")
			return &SSEEvent{
				Type: eventType,
				Data: json.RawMessage(dataStr),
			}, nil
		}
	}
	return nil, r.scanner.Err()
}

// Close closes the SSE reader.
func (r *SSEReader) Close() error {
	r.cancel()
	return r.resp.Body.Close()
}
