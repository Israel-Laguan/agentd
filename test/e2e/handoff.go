//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// humanResolutionRequest is the body for POST /api/v1/tasks/{id}/human-resolution
// (internal/api/controllers/tasks_human.go).
type humanResolutionRequest struct {
	Result string `json:"result"`
}

// ResolveHumanHandoff calls POST /api/v1/tasks/{taskID}/human-resolution.
// taskID is the HUMAN-assigned child task's ID (from ListTasks with
// includeHealing=true), not the parent it was created to unblock.
func (c *APIClient) ResolveHumanHandoff(ctx context.Context, taskID, result string) (*http.Response, error) {
	path := fmt.Sprintf("/api/v1/tasks/%s/human-resolution", taskID)
	return c.PostJSON(ctx, path, humanResolutionRequest{Result: result})
}

// HumanHandoffResolution is the response body of a resolved human handoff
// (internal/models.HumanHandoffResolution): the resolved HUMAN task and its
// now-completed parent.
type HumanHandoffResolution struct {
	Task   Task   `json:"task"`
	Parent Task   `json:"parent"`
	Result string `json:"result"`
}

// DecodeHumanHandoffResolution reads and JSON-decodes a human-resolution response.
func DecodeHumanHandoffResolution(resp *http.Response) (*HumanHandoffResolution, error) {
	defer func() { _ = resp.Body.Close() }()
	var envelope struct {
		Data HumanHandoffResolution `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode human handoff resolution: %w", err)
	}
	return &envelope.Data, nil
}
