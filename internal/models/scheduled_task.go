package models

import "time"

// ScheduledTaskKind distinguishes recurring dispatch from deferred requeue.
type ScheduledTaskKind string

const (
	ScheduledTaskKindDispatch ScheduledTaskKind = "dispatch"
	ScheduledTaskKindRequeue  ScheduledTaskKind = "requeue"
)

func (k ScheduledTaskKind) Valid() bool {
	return k == ScheduledTaskKindDispatch || k == ScheduledTaskKindRequeue
}

// ScheduledTask is a persisted scheduler registry entry.
type ScheduledTask struct {
	ID                  string
	CronExpr            string
	RunAfter            *time.Time
	TaskType            string
	ContextFn           string
	ContextArgs         map[string]string
	OutputTarget        string
	Title               string
	DescriptionTemplate string
	ProjectID           string
	Kind                ScheduledTaskKind
	TargetTaskID        string
	LastFiredAt         *time.Time
	Enabled             bool
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
