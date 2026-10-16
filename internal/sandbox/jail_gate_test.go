package sandbox

import (
	"strings"
	"testing"
)

// On CI the jail must run, so an unavailable jail fails the jail tests with the
// probe's reason instead of skipping them (B-049).
func TestJailVerdictOnCIFailsWithProbeReason(t *testing.T) {
	t.Setenv("CI", "true")
	fail, msg := jailVerdict(jailProbeResult{path: "/usr/bin/bwrap"})
	if !fail {
		t.Fatalf("jailVerdict() fail = false on CI, want true: a CI skip hides a broken jail")
	}
	for _, want := range []string{`bwrap path="/usr/bin/bwrap"`, "userns=false", "B-049"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("jailVerdict() msg = %q, want %q", msg, want)
		}
	}
}

// Off CI a laptop may lack a jail, so the same verdict is a skip.
func TestJailVerdictLocallySkipsWithProbeReason(t *testing.T) {
	t.Setenv("CI", "")
	fail, msg := jailVerdict(jailProbeResult{})
	if fail {
		t.Fatalf("jailVerdict() fail = true off CI, want skip")
	}
	for _, want := range []string{`bwrap path=""`, "userns=false"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("jailVerdict() msg = %q, want %q", msg, want)
		}
	}
}
