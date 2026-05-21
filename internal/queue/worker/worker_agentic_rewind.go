package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

const (
	rewindNone        = -1
	rewindToFirstTurn = 0 // distinct from rewindNone: restart from turn 0 after respec edit
	maxRewindStreak   = 3
)

var errRewindStagnation = errors.New("rewind stagnation")

// agenticRewindState tracks repeated rewinds to the same turn index.
type agenticRewindState struct {
	lastTarget int
	streak     int
}

// apply records a rewind request. Returns stagnation when the same target repeats
// more than maxRewindStreak times.
func (s *agenticRewindState) apply(rewindTo int) (stagnation bool) {
	if rewindTo == s.lastTarget {
		s.streak++
	} else {
		s.lastTarget = rewindTo
		s.streak = 1
	}
	return s.streak > maxRewindStreak
}

func (s *agenticRewindState) reset() {
	s.lastTarget = rewindNone
	s.streak = 0
}

func resetAgenticStateForRewind(in agenticTurnLoopInput) {
	if in.toolTracker != nil {
		in.toolTracker.reset()
	}
	if in.iterationGuard != nil {
		in.iterationGuard.reset()
	}
	if in.goalTracker != nil {
		if g := GoalFromTask(in.task); g != nil {
			in.goalTracker.SetGoal(*g)
		}
	}
}

type agenticTurnLoopInput struct {
	ctx              context.Context
	task             models.Task
	profile          models.AgentProfile
	messages         *[]gateway.PromptMessage
	tools            []gateway.ToolDefinition
	toolToAdapter    map[string]string
	taskToolExecutor *ToolExecutor
	iterationGuard   *IterationGuard
	budgetGuard      *BudgetGuard
	deadlineGuard    *DeadlineGuard
	ctxBudgetGuard   *ContextBudgetGuard
	cm               *ContextManager
	goalTracker      *GoalTracker
	taskHooks        *HookChain
	taskCaps         *capabilities.Registry
	toolTracker      *toolFailureTracker
	workPlan         *Plan
}

// runAgenticTurnLoop drives the inner agentic turn loop until completion, stagnation, or error.
func (w *Worker) runAgenticTurnLoop(in agenticTurnLoopInput) (LoopResult, bool) {
	respecAttempts := 0
	rewind := &agenticRewindState{}
	for turnIndex := 0; ; {
		turnID := fmt.Sprintf("%s:%d", in.task.ID, turnIndex)
		cont, result, report, rewindTo, err := w.processAgenticIteration(
			in.ctx, in.task, in.profile, in.messages, in.tools, in.toolToAdapter, in.taskToolExecutor,
			in.iterationGuard, in.budgetGuard, in.deadlineGuard, in.ctxBudgetGuard, in.cm, in.goalTracker,
			in.taskHooks, in.taskCaps, in.toolTracker, in.workPlan, turnID, turnIndex, &respecAttempts,
		)
		if err != nil {
			return LoopResult{}, false
		}
		if report {
			w.recordLoopResult(result)
			return result, true
		}
		if !cont {
			return LoopResult{}, false
		}
		if rewindTo >= 0 {
			if rewind.apply(rewindTo) {
				slog.Warn("agentic rewind stagnation",
					"task_id", in.task.ID,
					"turn_index", turnIndex,
					"rewind_to", rewindTo,
					"streak", rewind.streak,
				)
				r := LoopResult{
					Status: LoopTurnLimitExceeded,
					Meta: w.buildLoopMeta(
						turnIndex, in.budgetGuard.Usage(), totalChars(*in.messages), in.ctxBudgetGuard.TotalBudget(),
						errRewindStagnation.Error(), "", "",
					),
				}
				w.recordLoopResult(r)
				return r, true
			}
			turnIndex = rewindTo
			resetAgenticStateForRewind(in)
			continue
		}
		rewind.reset()
		turnIndex++
	}
}
