package acp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"agentd/internal/models"
)

// ErrModelMismatch is returned when session/new reports a model other than
// the configured one: an unknown id is not refused, the agent silently picks
// another (opencode 2.2.5), which may be paid.
var ErrModelMismatch = errors.New("acp: the session runs another model")

// quotaError matches quota and rate errors the agent reports as text.
var quotaError = regexp.MustCompile(`(?i)\b429\b|rate[ _-]?limit|too many requests|quota|insufficient (?:balance|credits)|usage limit`)

// fail names why op failed: the caller cancelled, the process died, the
// agent's provider refused for quota or rate (the breaker and defer path, the
// same as a gateway 429), or the agent's own error.
func (p *process) fail(ctx context.Context, op string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("acp: %s: %w", op, ctx.Err())
	}
	// The quota check names the agent's error before the dead-process branch:
	// a quota error from an agent that exits right after reporting it is still
	// a quota error, not a plain process exit.
	if quotaError.MatchString(err.Error()) {
		return fmt.Errorf("%w: acp agent: %s: %v", models.ErrLLMQuotaExceeded, op, err)
	}
	if p.dead() {
		select {
		case <-p.exited:
			return fmt.Errorf("acp: agent process exited during %s: %v (%v)", op, p.waitErr, err)
		case <-time.After(time.Second):
			return fmt.Errorf("acp: agent process exited during %s: %w", op, err)
		}
	}
	return fmt.Errorf("acp: %s: %w", op, err)
}
