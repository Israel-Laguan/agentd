package spec

import (
	"context"
	"strings"
	"testing"
)

func TestPurposeTaxonomyIsClosedAndWellFormed(t *testing.T) {
	all := AllPurposes()
	if len(all) != 25 {
		t.Fatalf("taxonomy has %d purposes, SP-023 approved 25", len(all))
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
		PurposeLegacyContractText:   LevelDrop,
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
