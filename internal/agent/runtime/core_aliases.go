package runtime

import "agentd/internal/queue/runaudit"

// The budget guard and the audit log live in queue/runaudit, where the core
// uses them (T-066); these aliases keep the harness compiling unchanged until
// it leaves.

type BudgetGuard = runaudit.BudgetGuard

var NewBudgetGuard = runaudit.NewBudgetGuard

type (
	AuditLogger        = runaudit.AuditLogger
	AuditSink          = runaudit.AuditSink
	FileAuditSink      = runaudit.FileAuditSink
	AuditRecord        = runaudit.AuditRecord
	HistoryEditRecord  = runaudit.HistoryEditRecord
	TurnSnapshotRecord = runaudit.TurnSnapshotRecord
	TaskAuditRecord    = runaudit.TaskAuditRecord
	DaemonStartRecord  = runaudit.DaemonStartRecord
)

var (
	NewAuditLogger   = runaudit.NewAuditLogger
	NewFileAuditSink = runaudit.NewFileAuditSink
	EnsureAuditFile  = runaudit.EnsureAuditFile
	HashArgs         = runaudit.HashArgs
)

const (
	RecordTypeToolDispatch  = runaudit.RecordTypeToolDispatch
	RecordTypeTurnSnapshot  = runaudit.RecordTypeTurnSnapshot
	RecordTypeHistoryEdit   = runaudit.RecordTypeHistoryEdit
	RecordTypeTaskStart     = runaudit.RecordTypeTaskStart
	RecordTypeTaskComplete  = runaudit.RecordTypeTaskComplete
	RecordTypeTaskFail      = runaudit.RecordTypeTaskFail
	RecordTypeTaskReview    = runaudit.RecordTypeTaskReview
	RecordTypeTaskSuspended = runaudit.RecordTypeTaskSuspended
	RecordTypeDaemonStart   = runaudit.RecordTypeDaemonStart
)
