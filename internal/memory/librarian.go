package memory

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// Librarian curates completed task logs into durable memories and manages
// the raw-log archival lifecycle.
type Librarian struct {
	Store   models.KanbanStore
	Gateway gateway.AIGateway
	Breaker gateway.BreakerChecker
	Sink    models.EventSink
	Cfg     config.LibrarianConfig
	HomeDir string
}

type memorySummary struct {
	Symptom  string `json:"symptom"`
	Solution string `json:"solution"`
}

var junkTokens = map[string]struct{}{
	"n/a": {}, "na": {}, "none": {}, "null": {}, "unknown": {},
	"empty": {}, "undefined": {}, "error": {}, "fail": {},
}

// IsMeaningful returns false when both symptom and solution collapse to
// whitespace, single noise tokens, or known junk markers (Danger A mitigation).
func (ms memorySummary) IsMeaningful() bool {
	s := strings.TrimSpace(ms.Symptom)
	o := strings.TrimSpace(ms.Solution)
	if s == "" && o == "" {
		return false
	}
	if isJunk(s) && isJunk(o) {
		return false
	}
	return true
}

func isJunk(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return true
	}
	_, ok := junkTokens[s]
	return ok
}

// CurateTask archives a completed task's events and writes a durable memory.
// Safety order: Archive -> RecordMemory -> MarkEventsCurated.
// The map-reduce approach trades more LLM calls (one per chunk plus reduce
// passes) for higher-fidelity summaries; librarian.chunk_chars controls the
// cost/quality tradeoff. When the breaker is open or the LLM fails, a
// deterministic head/tail extraction ensures curation never silently drops data.
func (l *Librarian) CurateTask(ctx context.Context, task models.Task) error {
	events, err := l.Store.ListEventsByTask(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("load events for task %s: %w", task.ID, err)
	}
	if len(events) == 0 {
		return nil
	}

	archivesDir := l.archivesDir()
	archivePath, err := WriteArchive(archivesDir, task.ProjectID, task.ID, events)
	if err != nil {
		return fmt.Errorf("archive task %s: %w", task.ID, err)
	}
	l.emitEvent(ctx, task, "LOG_ARCHIVED", fmt.Sprintf("path=%s events=%d", archivePath, len(events)))

	rawLog := assembleLog(events)

	summary, err := l.summarize(ctx, rawLog)
	if err != nil {
		slog.Warn("librarian LLM summarization failed, using fallback", "task", task.ID, "error", err)
		summary = l.fallbackExtract(rawLog)
	}

	if !summary.IsMeaningful() {
		l.emitEvent(ctx, task, "MEMORY_DISCARDED", fmt.Sprintf("task=%s reason=empty_or_junk", task.ID))
		if err := l.Store.MarkEventsCurated(ctx, task.ID); err != nil {
			return fmt.Errorf("mark events curated for task %s: %w", task.ID, err)
		}
		return nil
	}

	mem := models.Memory{
		Scope:     "TASK_CURATION",
		ProjectID: sql.NullString{String: task.ProjectID, Valid: true},
		Tags:      sql.NullString{String: "task_id:" + task.ID, Valid: true},
		Symptom:   sql.NullString{String: summary.Symptom, Valid: true},
		Solution:  sql.NullString{String: summary.Solution, Valid: true},
	}
	if err := l.Store.RecordMemory(ctx, mem); err != nil {
		return fmt.Errorf("record memory for task %s: %w", task.ID, err)
	}
	l.emitEvent(ctx, task, "MEMORY_INGESTED", fmt.Sprintf("task=%s symptom_len=%d solution_len=%d", task.ID, len(summary.Symptom), len(summary.Solution)))

	if project, err := l.Store.GetProject(ctx, task.ProjectID); err == nil && project != nil && project.WorkspacePath != "" {
		if writeErr := appendLessonMarkdown(project.WorkspacePath, task, mem); writeErr != nil {
			slog.Warn("failed to write lessons.md", "task", task.ID, "error", writeErr)
		}
	}

	if err := l.Store.MarkEventsCurated(ctx, task.ID); err != nil {
		return fmt.Errorf("mark events curated for task %s: %w", task.ID, err)
	}
	return nil
}

// CleanStaleArchives removes archives that have outlived the grace period
// and returns the purged archive identifiers.
func (l *Librarian) CleanStaleArchives() ([]PurgedArchive, error) {
	return CleanStaleArchives(l.archivesDir(), l.Cfg.ArchiveGraceDays)
}

// PurgeCuratedEvents deletes curated events for tasks whose archives have
// been cleaned past the grace period (Test 1 fidelity from spec).
func (l *Librarian) PurgeCuratedEvents(ctx context.Context, purged []PurgedArchive) error {
	for _, p := range purged {
		if err := l.Store.DeleteCuratedEvents(ctx, p.TaskID); err != nil {
			slog.Warn("purge curated events failed", "task", p.TaskID, "error", err)
			continue
		}
		if l.Sink != nil {
			_ = l.Sink.Emit(ctx, models.Event{
				ProjectID: p.ProjectID,
				TaskID:    sql.NullString{String: p.TaskID, Valid: true},
				Type:      "EVENTS_PURGED",
				Payload:   fmt.Sprintf("task=%s curated events deleted after archive grace period", p.TaskID),
			})
		}
	}
	return nil
}

func (l *Librarian) archivesDir() string {
	return l.HomeDir + "/archives"
}

func assembleLog(events []models.Event) string {
	var b strings.Builder
	for _, e := range events {
		fmt.Fprintf(&b, "[%s] %s\n%s\n\n", e.Type, e.CreatedAt.Format(time.RFC3339), e.Payload)
	}
	return b.String()
}

func (l *Librarian) emitEvent(ctx context.Context, task models.Task, eventType models.EventType, payload string) {
	if l.Sink == nil {
		return
	}
	_ = l.Sink.Emit(ctx, models.Event{
		ProjectID: task.ProjectID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      eventType,
		Payload:   payload,
	})
}

func (l *Librarian) chunkChars() int {
	if l.Cfg.ChunkChars > 0 {
		return l.Cfg.ChunkChars
	}
	return 8000
}

func (l *Librarian) maxReducePasses() int {
	if l.Cfg.MaxReducePasses > 0 {
		return l.Cfg.MaxReducePasses
	}
	return 3
}

func (l *Librarian) fallbackChars() int {
	if l.Cfg.FallbackHeadTailChars > 0 {
		return l.Cfg.FallbackHeadTailChars
	}
	return 2000
}
