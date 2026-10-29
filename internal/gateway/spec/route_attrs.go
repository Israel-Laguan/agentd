package spec

// TaskAttrs are the task facts a routing rule can match. The zero value is a
// call with none of them set, such as an intake call that has no task.
type TaskAttrs struct {
	// RequiresWorkspaceChange marks a task that must change the workspace, so a
	// run of it cannot finish on text alone (B-082). A nil value means the
	// fact is unset; matching skips any rule that conditions on it.
	RequiresWorkspaceChange *bool
}

