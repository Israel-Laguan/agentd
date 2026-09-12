package hooks

import "testing"

func TestNewHookChain_initializesNonNilEmptySlices(t *testing.T) {
	t.Parallel()

	hc := NewHookChain()
	if hc.preHooks == nil || len(hc.preHooks) != 0 {
		t.Errorf("preHooks = %#v, want non-nil empty slice", hc.preHooks)
	}
	if hc.postHooks == nil || len(hc.postHooks) != 0 {
		t.Errorf("postHooks = %#v, want non-nil empty slice", hc.postHooks)
	}
	if hc.sessionHooks == nil || len(hc.sessionHooks) != 0 {
		t.Errorf("sessionHooks = %#v, want non-nil empty slice", hc.sessionHooks)
	}
}
