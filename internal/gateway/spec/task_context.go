package spec

import "context"

type taskCtxKey struct{}

// WithTaskID attaches the task a call belongs to. Calls that run outside a
// request's own TaskID (subagent turns, librarian summaries, truncator
// summaries, plans) read it back with TaskIDFromContext. An empty value
// leaves ctx as it is.
func WithTaskID(ctx context.Context, taskID string) context.Context {
	if taskID == "" {
		return ctx
	}
	return context.WithValue(ctx, taskCtxKey{}, taskID)
}

// TaskIDFromContext returns the task set by WithTaskID, or "".
func TaskIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(taskCtxKey{}).(string); ok {
		return id
	}
	return ""
}
