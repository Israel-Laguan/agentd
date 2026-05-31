package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

const (
	RewindNone        = -1
	RewindToFirstTurn = 0
	MaxRewindStreak   = 3
)

var (
	ErrRewindStagnation = errors.New("rewind stagnation")
	ErrTopicDriftReset  = errors.New("topic drift: session reset")
)

// Runner executes an agent runtime request.
type Runner interface {
	Run(ctx context.Context, req Request) (Result, error)
}

// Result is the runtime-level outcome returned by Runner.
type Result struct {
	LoopResult LoopResult
	Reported   bool
}

// IterationState carries mutable per-loop retry/recovery counters that the
// iteration callback can pass down to lower-level runtime steps.
type IterationState struct {
	RespecAttempts                 int
	SessionRecoveryGen             int
	SessionRecoveryUsed            bool
	SessionRecoveryNeedsPlanInject bool
}

// IterationOutcome describes one completed turn-loop iteration.
type IterationOutcome struct {
	Continue bool
	Result   LoopResult
	Report   bool
	RewindTo int
}

// Request contains the narrow callbacks needed by the generic turn-loop
// runner. Queue persistence and task lifecycle side effects stay behind these
// callbacks in the worker adapter for now.
type Request struct {
	TaskID string
	Ports  Ports

	Iterate                        func(ctx context.Context, turnID string, turnIndex int, state *IterationState) (IterationOutcome, error)
	ResetForRewind                 func()
	ResetForTopicDrift             func()
	ApplySessionRecoveryPlanInject func(state *IterationState)
	RecordResult                   func(LoopResult)
	BuildRewindStagnationResult    func(turnIndex int) LoopResult
}

// TurnLoopRunner drives the queue-agnostic turn-loop control flow.
type TurnLoopRunner struct{}

func NewTurnLoopRunner() *TurnLoopRunner {
	return &TurnLoopRunner{}
}

func (r *TurnLoopRunner) Run(ctx context.Context, req Request) (Result, error) {
	if req.Iterate == nil {
		return Result{}, errors.New("runtime runner: nil iterate callback")
	}
	state := &IterationState{}
	rewind := &rewindState{}
	for turnIndex := 0; ; {
		turnID := fmt.Sprintf("%s:%d", req.TaskID, turnIndex)
		outcome, err := req.Iterate(ctx, turnID, turnIndex, state)
		if err != nil {
			if errors.Is(err, ErrTopicDriftReset) && outcome.RewindTo >= 0 {
				rewind.reset()
				turnIndex = outcome.RewindTo
				if req.ResetForTopicDrift != nil {
					req.ResetForTopicDrift()
				}
				continue
			}
			return Result{}, err
		}
		if outcome.Report {
			recordResult(req, outcome.Result)
			return Result{LoopResult: outcome.Result, Reported: true}, nil
		}
		if !outcome.Continue {
			return Result{}, nil
		}
		if outcome.RewindTo >= 0 {
			if rewind.apply(outcome.RewindTo) {
				slog.Warn("agentic rewind stagnation",
					"task_id", req.TaskID,
					"turn_index", turnIndex,
					"rewind_to", outcome.RewindTo,
					"streak", rewind.streak,
				)
				result := LoopResult{
					Status: LoopTurnLimitExceeded,
					Meta:   LoopMeta{TurnCount: turnIndex, LastError: ErrRewindStagnation.Error()},
				}
				if req.BuildRewindStagnationResult != nil {
					result = req.BuildRewindStagnationResult(turnIndex)
				}
				recordResult(req, result)
				return Result{LoopResult: result, Reported: true}, nil
			}
			turnIndex = outcome.RewindTo
			if req.ResetForRewind != nil {
				req.ResetForRewind()
			}
			if req.ApplySessionRecoveryPlanInject != nil {
				req.ApplySessionRecoveryPlanInject(state)
			}
			continue
		}
		rewind.reset()
		turnIndex++
	}
}

func recordResult(req Request, result LoopResult) {
	if req.RecordResult != nil {
		req.RecordResult(result)
	}
}

const maxRecentWindow = 5

type rewindState struct {
	lastTarget    int
	streak        int
	recentTargets []int
}

func (s *rewindState) apply(rewindTo int) (stagnation bool) {
	// Track recent targets in a sliding window to detect alternating stagnation.
	s.recentTargets = append(s.recentTargets, rewindTo)
	if len(s.recentTargets) > maxRecentWindow {
		s.recentTargets = s.recentTargets[len(s.recentTargets)-maxRecentWindow:]
	}

	// Check for alternating stagnation: if rewindTo appears in recent targets,
	// we're oscillating between at least two targets.
	occurrences := 0
	for _, t := range s.recentTargets {
		if t == rewindTo {
			occurrences++
		}
	}
	if occurrences >= 2 {
		s.streak++
	} else if rewindTo == s.lastTarget {
		s.streak++
	} else {
		s.lastTarget = rewindTo
		s.streak = 1
	}
	return s.streak > MaxRewindStreak
}

func (s *rewindState) reset() {
	s.lastTarget = RewindNone
	s.streak = 0
	s.recentTargets = nil
}
