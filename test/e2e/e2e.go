//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Harness manages the devenv stack and executes journeys.
type Harness struct {
	baseURL string
	client  *http.Client
	profile string
}

// NewHarness creates a new harness for the given profile.
// The profile must match one defined in devenv/compose.yaml.
func NewHarness(baseURL, profile string) *Harness {
	return &Harness{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		profile: profile,
	}
}

// WaitForHealthy polls the system/status endpoint until the service is ready.
// Returns an error if the timeout is exceeded.
func (h *Harness) WaitForHealthy(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Until(deadline)):
			return fmt.Errorf("harness not healthy after %v", timeout)
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet,
				fmt.Sprintf("%s/api/v1/system/status", h.baseURL), nil)
			if err != nil {
				continue
			}

			resp, err := h.client.Do(req)
			if err != nil {
				continue
			}
			_ = resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
}

// Get makes a GET request to the API.
func (h *Harness) Get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s%s", h.baseURL, path), nil)
	if err != nil {
		return nil, err
	}
	return h.client.Do(req)
}

// BaseURL returns the harness base URL.
func (h *Harness) BaseURL() string {
	return h.baseURL
}
