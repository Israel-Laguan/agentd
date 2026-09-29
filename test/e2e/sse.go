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

// SSEEvent represents a server-sent event from /api/v1/events/stream.
//
// Type is the lowercased event name from the frame's "event:" line, which
// internal/api/sse/stream.go's eventName maps from the internal type (RESULT
// becomes "task_updated", LOG_CHUNK becomes "log_chunk", and anything
// unmapped falls through to strings.ToLower of the internal type).
type SSEEvent struct {
	Type string          // e.g. "log_chunk", "task_updated", "warning"
	Data json.RawMessage // Raw JSON data
}

// signalPayload is the decoded body of an SSE frame's "data:" line
// (internal/api/sse/stream.go's writeEvent marshals exactly these three keys).
type signalPayload struct {
	Topic   string `json:"topic"`
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

// Signal decodes the event's data line into its topic/type/payload.
func (e *SSEEvent) Signal() (signalPayload, error) {
	var out signalPayload
	if err := json.Unmarshal(e.Data, &out); err != nil {
		return out, fmt.Errorf("decode sse signal: %w", err)
	}
	return out, nil
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

// DrainEvents reads events until timeout elapses with no new event, returning
// everything received in arrival order. This suits J14, which asserts on a
// burst of lifecycle signals and their relative order, rather than on any
// single event arriving within a window (what NextEvent is for).
//
// A short quiet period is treated as end-of-stream: the frame sequence for a
// completing task arrives within milliseconds, so an idle gap means the burst
// is over rather than that more are coming.
func (r *SSEReader) DrainEvents(ctx context.Context, quietPeriod, maxWait time.Duration) []SSEEvent {
	var events []SSEEvent
	deadline := time.Now().Add(maxWait)

	for time.Now().Before(deadline) {
		event, err := r.NextEvent(ctx, quietPeriod)
		if err != nil {
			return events
		}
		events = append(events, *event)
	}
	return events
}

// Close closes the SSE reader.
func (r *SSEReader) Close() error {
	r.cancel()
	return r.resp.Body.Close()
}
