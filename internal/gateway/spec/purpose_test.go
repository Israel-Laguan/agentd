package spec

import (
	"context"
	"strings"
	"testing"
)

func TestPurposeTaxonomyIsClosedAndWellFormed(t *testing.T) {
	all := AllPurposes()
	// T-055a removed the two legacy.contract_* purposes (no production caller);
	// the healing split then moved off exec.legacy_command to create.split_task.
	if len(all) != 24 {
		t.Fatalf("taxonomy has %d purposes, want 24", len(all))
	}
	for _, p := range all {
		if strings.HasPrefix(string(p), "legacy.") {
			t.Errorf("legacy purpose %q is still in the taxonomy", p)
		}
	}
	seen := map[Purpose]bool{}
	for _, p := range all {
		if seen[p] {
			t.Errorf("duplicate purpose %q", p)
		}
		seen[p] = true
		stage, name, ok := strings.Cut(string(p), ".")
		if !ok || stage == "" || name == "" {
			t.Errorf("purpose %q is not <stage>.<name>", p)
		}
		if p.Stage() != stage {
			t.Errorf("%q: Stage() = %q, want %q", p, p.Stage(), stage)
		}
		if !p.Valid() {
			t.Errorf("%q: Valid() = false", p)
		}
		if p.MinLevel() == "" {
			t.Errorf("%q: no provisional minimum level", p)
		}
	}
}

func TestUnlabelledIsReservedAndNotInTheSet(t *testing.T) {
	for _, p := range AllPurposes() {
		if p == PurposeUnlabelled {
			t.Fatal("unlabelled must not be a call-site purpose")
		}
	}
	if PurposeUnlabelled.Valid() {
		t.Error("unlabelled must not be Valid: call sites may not send it")
	}
	if Purpose("").Normalize() != PurposeUnlabelled {
		t.Error("empty purpose must normalize to unlabelled")
	}
	if Purpose("nope.nothing").Normalize() != PurposeUnlabelled {
		t.Error("unknown purpose must normalize to unlabelled, never be dropped")
	}
	if PurposeExecTurn.Normalize() != PurposeExecTurn {
		t.Error("known purpose must survive Normalize")
	}
}

func TestSP023Levels(t *testing.T) {
	want := map[Purpose]Level{
		PurposeExecTurn:             LevelL3,
		PurposeSubagentTurn:         LevelL3,
		PurposeIntakeClassifyIntent: LevelL1,
		PurposeSystemWarmup:         LevelNone,
	}
	for p, lvl := range want {
		if p.MinLevel() != lvl {
			t.Errorf("%q: level %q, want %q", p, p.MinLevel(), lvl)
		}
	}
}

func TestPurposeContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	if PurposeFromContext(ctx) != "" {
		t.Error("empty context must carry no purpose")
	}
	ctx = WithPurpose(ctx, PurposeCreateNextPhase)
	if PurposeFromContext(ctx) != PurposeCreateNextPhase {
		t.Error("purpose lost in context")
	}
}
