package runtime

import "context"

type Goal struct {
	ID          string
	Description string
}

type Ports interface {
	RecordTokenUsage(tokens int)
	EmitToolCall(callID, name, args string)
	EmitToolResult(callID, content string)
	HandleGatewayError(ctx context.Context, err error) error
	HandleGoalStalled(ctx context.Context) error
	IngestHumanCorrections(ctx context.Context)
	TryFinalizeApprovedReview(ctx context.Context) (bool, error)
	CommitTextWithProfile(ctx context.Context, text string)
	PersistGoalCriteria(goals []Goal) error
	EmitEvent(eventType, payload string)
}

type PortsFuncs struct {
	recordTokenUsage         func(tokens int)
	emitToolCall             func(callID, name, args string)
	emitToolResult           func(callID, content string)
	handleGatewayError       func(ctx context.Context, err error) error
	handleGoalStalled        func(ctx context.Context) error
	ingestHumanCorrections   func(ctx context.Context)
	tryFinalizeApprovedReview func(ctx context.Context) (bool, error)
	commitTextWithProfile    func(ctx context.Context, text string)
	persistGoalCriteria      func(goals []Goal) error
	emitEvent                func(eventType, payload string)
}

func (p *PortsFuncs) RecordTokenUsage(tokens int) {
	if p.recordTokenUsage != nil {
		p.recordTokenUsage(tokens)
	}
}

func (p *PortsFuncs) EmitToolCall(callID, name, args string) {
	if p.emitToolCall != nil {
		p.emitToolCall(callID, name, args)
	}
}

func (p *PortsFuncs) EmitToolResult(callID, content string) {
	if p.emitToolResult != nil {
		p.emitToolResult(callID, content)
	}
}

func (p *PortsFuncs) HandleGatewayError(ctx context.Context, err error) error {
	if p.handleGatewayError != nil {
		return p.handleGatewayError(ctx, err)
	}
	return err
}

func (p *PortsFuncs) HandleGoalStalled(ctx context.Context) error {
	if p.handleGoalStalled != nil {
		return p.handleGoalStalled(ctx)
	}
	return nil
}

func (p *PortsFuncs) IngestHumanCorrections(ctx context.Context) {
	if p.ingestHumanCorrections != nil {
		p.ingestHumanCorrections(ctx)
	}
}

func (p *PortsFuncs) TryFinalizeApprovedReview(ctx context.Context) (bool, error) {
	if p.tryFinalizeApprovedReview != nil {
		return p.tryFinalizeApprovedReview(ctx)
	}
	return false, nil
}

func (p *PortsFuncs) CommitTextWithProfile(ctx context.Context, text string) {
	if p.commitTextWithProfile != nil {
		p.commitTextWithProfile(ctx, text)
	}
}

func (p *PortsFuncs) PersistGoalCriteria(goals []Goal) error {
	if p.persistGoalCriteria != nil {
		return p.persistGoalCriteria(goals)
	}
	return nil
}

func (p *PortsFuncs) EmitEvent(eventType, payload string) {
	if p.emitEvent != nil {
		p.emitEvent(eventType, payload)
	}
}

var _ Ports = (*PortsFuncs)(nil)