package kanban

import (
	"context"
	"fmt"

	"agentd/internal/models"
)

// ToolUseReport counts tool calls by tool and finds the files one task read
// more than once.
func (s *Store) ToolUseReport(ctx context.Context, q models.ToolUseQuery) (models.ToolUseReport, error) {
	where, args := toolUseWhere(q)
	report := models.ToolUseReport{Tools: []models.ToolUseRow{}, RepeatedReads: []models.RepeatedRead{}}
	rows, err := s.db.QueryContext(ctx, `SELECT tool, COUNT(*), COALESCE(SUM(status != 'ok'), 0), COALESCE(SUM(result_bytes), 0)
		FROM tool_calls WHERE `+where+` GROUP BY tool ORDER BY COUNT(*) DESC, tool`, args...)
	if err != nil {
		return report, fmt.Errorf("query tool use: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r models.ToolUseRow
		if err := rows.Scan(&r.Tool, &r.Calls, &r.Failed, &r.ResultBytes); err != nil {
			return report, fmt.Errorf("scan tool use: %w", err)
		}
		if r.Tool == "read" {
			report.Reads, report.ReadBytes = r.Calls, r.ResultBytes
		}
		report.Tools = append(report.Tools, r)
	}
	if err := rows.Err(); err != nil {
		return report, fmt.Errorf("iterate tool use: %w", err)
	}
	return report, s.repeatedReads(ctx, where, args, &report)
}

// repeatedReads fills the repeated reads: per task and path with more than one
// read, the bytes of every read after the first.
func (s *Store) repeatedReads(ctx context.Context, where string, args []any, report *models.ToolUseReport) error {
	rows, err := s.db.QueryContext(ctx, `SELECT t.task_id, t.path, COUNT(*), SUM(t.result_bytes),
		SUM(t.result_bytes) - (SELECT f.result_bytes FROM tool_calls f
			WHERE f.task_id = t.task_id AND f.path = t.path AND f.tool = 'read' ORDER BY f.id LIMIT 1)
		FROM tool_calls t WHERE `+where+` AND t.tool = 'read' AND t.path != ''
		GROUP BY t.task_id, t.path HAVING COUNT(*) > 1 ORDER BY 5 DESC, t.task_id, t.path`, args...)
	if err != nil {
		return fmt.Errorf("query repeated reads: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r models.RepeatedRead
		if err := rows.Scan(&r.TaskID, &r.Path, &r.Reads, &r.Bytes, &r.RereadBytes); err != nil {
			return fmt.Errorf("scan repeated read: %w", err)
		}
		report.RereadBytes += r.RereadBytes
		if len(report.RepeatedReads) < topRepeatedReads {
			report.RepeatedReads = append(report.RepeatedReads, r)
		}
	}
	return rows.Err()
}
