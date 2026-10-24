package spec

import "context"

type taskCtxKey struct{}

// WithTaskID attaches the task a call belongs to. Calls that run outside a
// request's own TaskID (subagent turns, librarian summaries, truncator
// summaries) read it back with TaskIDFromContext. An empty value leaves the
// call unlabelled.
func WithTaskID(ctx context.Context, taskID string) context.Context {
	return context.WithValue(ctx, taskCtxKey{}, taskID)
}

// TaskIDFromContext returns the task set by WithTaskID, or "".
func TaskIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(taskCtxKey{}).(string)
	return id
}
