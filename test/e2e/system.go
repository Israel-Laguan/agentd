//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// SystemProjectID is the fixed UUID of agentd's `_system` project
// (internal/kanban/system_project.go's systemProjectID). The disk watchdog
// (internal/queue/disk_watchdog.go) and the outage handoff
// (internal/queue/outage_handoff.go) both attach their HUMAN tasks here, and
// `_system` is excluded from GET /api/v1/projects unless include_system=true
// — but it is addressable directly by ID, which is how a journey observes it.
const SystemProjectID = "00000000-0000-0000-0000-000000000001"

// Breaker states, mirroring internal/queue/safety.BreakerState.
const (
	BreakerClosed   = "CLOSED"
	BreakerOpen     = "OPEN"
	BreakerHalfOpen = "HALF_OPEN"
)

// BreakerSnapshot is the `breaker` field of the system/status response
// (internal/services.BreakerSnapshot). OpenFor is a bare time.Duration, so it
// serializes as an integer nanosecond count rather than "5m0s".
type BreakerSnapshot struct {
	State        string `json:"state"`
	FailureCount int    `json:"failure_count"`
	OpenFor      int64  `json:"open_for"`
	LastError    string `json:"last_error"`
}

// ProviderBreaker is one entry of the status response's provider_breakers map
// (internal/services.ProviderBreakerEntry), keyed by provider name. Only
// quota errors (ErrLLMQuotaExceeded) feed per-provider breakers; unreachable
// providers feed the single global breaker — see
// internal/queue/worker/worker_handoffs.go's HandleGatewayError.
type ProviderBreaker struct {
	State        string `json:"state"`
	FailureCount int    `json:"failure_count"`
}

// SystemStatusReport is the subset of GET /api/v1/system/status that journeys
// assert on. The endpoint also carries frontdesk status, memory, and rolling
// budget fields, which are not journey-relevant.
type SystemStatusReport struct {
	Breaker          *BreakerSnapshot           `json:"breaker"`
	ProviderBreakers map[string]ProviderBreaker `json:"provider_breakers"`
	RollingBudgetOn  bool                       `json:"rolling_budget_enabled"`
}

// DecodeSystemStatus reads and decodes a system/status response.
func DecodeSystemStatus(resp *http.Response) (*SystemStatusReport, error) {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("system status returned %d", resp.StatusCode)
	}
	var envelope struct {
		Data SystemStatusReport `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode system status: %w", err)
	}
	return &envelope.Data, nil
}

// SystemProjectTasks calls GET /api/v1/projects/_system/tasks and decodes the
// result. Used by J10 to observe the disk watchdog's HUMAN task. Pass
// includeHealing so tasks created by the self-healing ladder are listed (the
// disk task is HUMAN but its title is not the "Manual review required:"
// prefix, so it is listed either way).
func (c *APIClient) SystemProjectTasks(ctx context.Context, includeHealing bool) ([]Task, error) {
	path := SystemProjectID + "/tasks"
	if includeHealing {
		path += "?include_healing=true"
	}
	resp, err := c.Get(ctx, "/api/v1/projects/"+path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list _system tasks returned %d", resp.StatusCode)
	}
	var envelope struct {
		Data []Task `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode _system tasks: %w", err)
	}
	return envelope.Data, nil
}
