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
	RecordTokenUsageFn         func(tokens int)
	EmitToolCallFn             func(callID, name, args string)
	EmitToolResultFn           func(callID, content string)
	HandleGatewayErrorFn       func(ctx context.Context, err error) error
	HandleGoalStalledFn        func(ctx context.Context) error
	IngestHumanCorrectionsFn   func(ctx context.Context)
	TryFinalizeApprovedReviewFn func(ctx context.Context) (bool, error)
	CommitTextWithProfileFn    func(ctx context.Context, text string)
	PersistGoalCriteriaFn      func(goals []Goal) error
	EmitEventFn                func(eventType, payload string)
}

func (p *PortsFuncs) RecordTokenUsage(tokens int) {
	if p.RecordTokenUsageFn != nil {
		p.RecordTokenUsageFn(tokens)
	}
}

func (p *PortsFuncs) EmitToolCall(callID, name, args string) {
	if p.EmitToolCallFn != nil {
		p.EmitToolCallFn(callID, name, args)
	}
}

func (p *PortsFuncs) EmitToolResult(callID, content string) {
	if p.EmitToolResultFn != nil {
		p.EmitToolResultFn(callID, content)
	}
}

func (p *PortsFuncs) HandleGatewayError(ctx context.Context, err error) error {
	if p.HandleGatewayErrorFn != nil {
		return p.HandleGatewayErrorFn(ctx, err)
	}
	return err
}

func (p *PortsFuncs) HandleGoalStalled(ctx context.Context) error {
	if p.HandleGoalStalledFn != nil {
		return p.HandleGoalStalledFn(ctx)
	}
	return nil
}

func (p *PortsFuncs) IngestHumanCorrections(ctx context.Context) {
	if p.IngestHumanCorrectionsFn != nil {
		p.IngestHumanCorrectionsFn(ctx)
	}
}

func (p *PortsFuncs) TryFinalizeApprovedReview(ctx context.Context) (bool, error) {
	if p.TryFinalizeApprovedReviewFn != nil {
		return p.TryFinalizeApprovedReviewFn(ctx)
	}
	return false, nil
}

func (p *PortsFuncs) CommitTextWithProfile(ctx context.Context, text string) {
	if p.CommitTextWithProfileFn != nil {
		p.CommitTextWithProfileFn(ctx, text)
	}
}

func (p *PortsFuncs) PersistGoalCriteria(goals []Goal) error {
	if p.PersistGoalCriteriaFn != nil {
		return p.PersistGoalCriteriaFn(goals)
	}
	return nil
}

func (p *PortsFuncs) EmitEvent(eventType, payload string) {
	if p.EmitEventFn != nil {
		p.EmitEventFn(eventType, payload)
	}
}

var _ Ports = (*PortsFuncs)(nil)