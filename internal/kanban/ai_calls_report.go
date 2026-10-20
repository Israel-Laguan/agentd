package kanban

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/models"
)

var _ models.AICallStore = (*Store)(nil)

// aiDimensionSQL maps a report dimension to its column expression. It is the
// only text ever interpolated into the report query.
var aiDimensionSQL = map[string]string{
	models.AIDimPurpose: "purpose",
	models.AIDimStage:   "stage",
	models.AIDimModel:   "model",
	models.AIDimDay:     "substr(at, 1, 10)",
	models.AIDimTask:    "task_id",
	models.AIDimRequest: "correlation_id",
}

const aiAggregateColumns = `COUNT(*),
	COALESCE(SUM(outcome = 'failed'), 0),
	COALESCE(SUM(tokens), 0), COALESCE(SUM(cached_tokens), 0),
	COALESCE(SUM(input_chars), 0), COALESCE(SUM(output_chars), 0),
	COALESCE(AVG(latency_ms), 0), COALESCE(MAX(latency_ms), 0),
	COALESCE(SUM(attempt = 'first'), 0), COALESCE(SUM(attempt = 'json_repair'), 0),
	COALESCE(SUM(cascade), 0)`

// AIUsageReport aggregates ai_calls by the requested dimensions, plus a totals
// row over the same filter. Dropped is left for the service to fill.
func (s *Store) AIUsageReport(ctx context.Context, q models.AIUsageQuery) (models.AIUsageReport, error) {
	if err := q.Validate(); err != nil {
		return models.AIUsageReport{}, err
	}
	where, args := aiUsageWhere(q)
	rows, err := s.aiAggregate(ctx, where, args, q.GroupBy)
	if err != nil {
		return models.AIUsageReport{}, err
	}
	totals, err := s.aiAggregate(ctx, where, args, nil)
	if err != nil {
		return models.AIUsageReport{}, err
	}
	report := models.AIUsageReport{GroupBy: append([]string{}, q.GroupBy...), Rows: rows}
	if len(totals) > 0 {
		report.Totals = totals[0]
	}
	return report, nil
}

func aiUsageWhere(q models.AIUsageQuery) (string, []any) {
	var conds []string
	var args []any
	add := func(cond string, arg any) {
		conds = append(conds, cond)
		args = append(args, arg)
	}
	if !q.From.IsZero() {
		add("at >= ?", formatTime(q.From))
	}
	if !q.To.IsZero() {
		add("at < ?", formatTime(q.To))
	}
	for col, v := range map[string]string{
		"task_id": q.TaskID, "correlation_id": q.CorrelationID,
		"purpose": q.Purpose, "stage": q.Stage, "model": q.Model,
	} {
		if v != "" {
			add(col+" = ?", v)
		}
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func (s *Store) aiAggregate(ctx context.Context, where string, args []any, dims []string) ([]models.AIUsageRow, error) {
	exprs := make([]string, len(dims))
	for i, d := range dims {
		exprs[i] = aiDimensionSQL[d]
	}
	selectCols := aiAggregateColumns
	suffix := ""
	if len(exprs) > 0 {
		selectCols = strings.Join(exprs, ", ") + ", " + aiAggregateColumns
		suffix = " GROUP BY " + strings.Join(exprs, ", ") + " ORDER BY " + aiOrder(dims, exprs)
	}
	rs, err := s.db.QueryContext(ctx, "SELECT "+selectCols+" FROM ai_calls"+where+suffix, args...)
	if err != nil {
		return nil, fmt.Errorf("query ai usage: %w", err)
	}
	defer func() { _ = rs.Close() }()
	out := []models.AIUsageRow{}
	for rs.Next() {
		row, err := scanAIUsageRow(rs.Scan, dims)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rs.Err(); err != nil {
		return nil, fmt.Errorf("iterate ai usage: %w", err)
	}
	return out, nil
}

// aiOrder lists days oldest first when grouped by day, and the biggest token
// spenders first otherwise.
func aiOrder(dims, exprs []string) string {
	for i, d := range dims {
		if d == models.AIDimDay {
			return exprs[i] + ", SUM(tokens) DESC"
		}
	}
	return "SUM(tokens) DESC, " + strings.Join(exprs, ", ")
}

func scanAIUsageRow(scan func(...any) error, dims []string) (models.AIUsageRow, error) {
	var row models.AIUsageRow
	vals := make([]string, len(dims))
	dest := make([]any, 0, len(dims)+11)
	for i := range vals {
		dest = append(dest, &vals[i])
	}
	dest = append(dest, &row.Calls, &row.Failed, &row.Tokens, &row.CachedTokens, &row.InputChars,
		&row.OutputChars, &row.AvgLatencyMS, &row.MaxLatencyMS, &row.FirstAttempts, &row.RepairAttempts, &row.CascadeCalls)
	if err := scan(dest...); err != nil {
		return row, fmt.Errorf("scan ai usage row: %w", err)
	}
	if len(dims) > 0 {
		row.Group = make(map[string]string, len(dims))
		for i, d := range dims {
			row.Group[d] = vals[i]
		}
	}
	if row.FirstAttempts > 0 {
		row.RepairRate = float64(row.RepairAttempts) / float64(row.FirstAttempts)
	}
	if row.Calls > 0 {
		row.CascadeRate = float64(row.CascadeCalls) / float64(row.Calls)
	}
	return row, nil
}
