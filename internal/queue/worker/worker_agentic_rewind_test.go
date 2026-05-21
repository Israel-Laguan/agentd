package worker

import "testing"

func TestAgenticRewindState_Stagnation(t *testing.T) {
	t.Parallel()
	s := &agenticRewindState{}
	for i := 0; i < maxRewindStreak; i++ {
		if s.apply(rewindToFirstTurn) {
			t.Fatalf("stagnation on apply %d, want only after streak > %d", i+1, maxRewindStreak)
		}
	}
	if !s.apply(rewindToFirstTurn) {
		t.Fatalf("expected stagnation after %d rewinds to same target", maxRewindStreak+1)
	}
}

func TestAgenticRewindState_DifferentTargetResetsStreak(t *testing.T) {
	t.Parallel()
	s := &agenticRewindState{}
	s.apply(rewindToFirstTurn)
	s.apply(rewindToFirstTurn)
	if s.apply(1) {
		t.Fatal("different rewind target should not stagnate on first apply")
	}
	if s.streak != 1 {
		t.Fatalf("streak = %d, want 1 after target change", s.streak)
	}
}

func TestAgenticRewindState_ForwardProgressResetsStreak(t *testing.T) {
	t.Parallel()
	s := &agenticRewindState{}
	s.apply(rewindToFirstTurn)
	s.apply(rewindToFirstTurn)
	if s.streak != 2 {
		t.Fatalf("streak = %d, want 2 before forward progress reset", s.streak)
	}
	s.reset()
	if s.apply(rewindToFirstTurn) {
		t.Fatal("forward progress reset should clear streak; same target after reset must not stagnate")
	}
	if s.streak != 1 {
		t.Fatalf("streak = %d, want 1 after reset and re-apply", s.streak)
	}
}
