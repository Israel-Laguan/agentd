//go:build !fakeacp

package acp

import "testing"

// Only the devenv image (-tags fakeacp) has the fake preset.
func TestProductionBuildHasNoFakePreset(t *testing.T) {
	if _, ok := Presets["fake"]; ok {
		t.Fatal("a build without -tags fakeacp registers the fake preset")
	}
}
