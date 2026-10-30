package spec

// TaskAttrs are the task facts a routing rule can match. The zero value is a
// call with none of them set, such as an intake call that has no task.
type TaskAttrs struct {
	// RequiresWorkspaceChange marks a task that must change the workspace, so a
	// run of it cannot finish on text alone (B-082). A nil value means the
	// fact is unset; matching skips any rule that conditions on it.
	RequiresWorkspaceChange *bool
}

// AttrsForTask returns the routing attributes of a call made for one task.
// Every single-task request sets them, so a rule can match its purpose. A
// batch call covers several tasks and carries none, so rules that condition on
// an attribute never match it.
func AttrsForTask(requiresWorkspaceChange bool) TaskAttrs {
	return TaskAttrs{RequiresWorkspaceChange: &requiresWorkspaceChange}
}
