package hooks

import (
	"path/filepath"
	"testing"
)

// A project ID is one path element. One that climbs (p1/../p2) or names the
// projects dir itself (".", "..") would move the denylist root, so the hook
// refuses the call instead of resolving paths against it.
func TestDenylistHookFor_RefusesProjectIDsThatMoveTheRoot(t *testing.T) {
	hook := DenylistHookFor(func(projectID string) string {
		return filepath.Join("/projects", projectID)
	})
	for _, id := range []string{"p1/../p2", "../p2", "p1/sub", ".", "..", `p1\..\p2`} {
		verdict, err := hook.Fn(HookContext{ToolName: "read", ProjectID: id, Args: `{"path":"secret"}`})
		if err != nil {
			t.Fatalf("project %q: %v", id, err)
		}
		if !verdict.Veto {
			t.Errorf("project %q: a read was allowed under a root it moved", id)
		}
	}
	for _, id := range []string{"p1", "3f2b8c1e-uuid", ""} {
		if verdict, _ := hook.Fn(HookContext{ToolName: "read", ProjectID: id, Args: `{"path":"secret"}`}); verdict.Veto {
			t.Errorf("project %q: refused a plain in-workspace read: %s", id, verdict.Reason)
		}
	}
}
