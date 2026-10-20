package kanban

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/models"
)

var _ models.ToolCallStore = (*Store)(nil)

// topRepeatedReads is how many repeated reads the report lists; the totals
// cover all of them.
const topRepeatedReads = 20

// InsertToolCall records one tool dispatch.
func (s *Store) InsertToolCall(ctx context.Context, c models.ToolCall) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO tool_calls (at, task_id, call_id, tool, path, status, result_bytes)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, formatTime(c.At), c.TaskID, c.CallID, c.Tool, c.Path, c.Status, c.ResultBytes)
	if err != nil {
		return fmt.Errorf("insert tool_call: %w", err)
	}
	return nil
}

func toolUseWhere(q models.ToolUseQuery) (string, []any) {
	conds, args := []string{"1=1"}, []any{}
	if !q.From.IsZero() {
		conds, args = append(conds, "at >= ?"), append(args, formatTime(q.From))
	}
	if !q.To.IsZero() {
		conds, args = append(conds, "at < ?"), append(args, formatTime(q.To))
	}
	if q.TaskID != "" {
		conds, args = append(conds, "task_id = ?"), append(args, q.TaskID)
	}
	return strings.Join(conds, " AND "), args
}
