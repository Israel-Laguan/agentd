package truncation

import (
	"strings"
	"testing"
)

func TestHeadTailShortInput(t *testing.T) {
	got := HeadTailStrategy{HeadRatio: 0.5}.Truncate("short", 20)
	if got != "short" {
		t.Fatalf("HeadTailStrategy.Truncate() = %q, want short", got)
	}
}

func TestHeadTailDynamicRatios(t *testing.T) {
	input := strings.Repeat("a", 50) + strings.Repeat("b", 50)
	budget := 40
	remaining := budget - len(truncationMarker)

	tests := []struct {
		name      string
		ratio     float64
		wantHead  int
		wantTail  int
		wantTotal int
	}{
		{name: "tail only", ratio: 0, wantHead: 0, wantTail: remaining, wantTotal: budget},
		{name: "equal split", ratio: 0.5, wantHead: remaining / 2, wantTail: remaining - remaining/2, wantTotal: budget},
		{name: "mostly head", ratio: 0.8, wantHead: int(float64(remaining) * 0.8), wantTail: remaining - int(float64(remaining)*0.8), wantTotal: budget},
		{name: "head only", ratio: 1, wantHead: remaining, wantTail: 0, wantTotal: budget},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HeadTailStrategy{HeadRatio: tt.ratio}.Truncate(input, budget)
			if len(got) != tt.wantTotal {
				t.Fatalf("len(got) = %d, want %d", len(got), tt.wantTotal)
			}
			if !strings.Contains(got, truncationMarker) {
				t.Fatalf("got %q missing marker %q", got, truncationMarker)
			}
			if tt.wantHead > 0 && !strings.HasPrefix(got, input[:tt.wantHead]) {
				t.Fatalf("got %q does not preserve requested head", got)
			}
			if tt.wantTail > 0 && !strings.HasSuffix(got, input[len(input)-tt.wantTail:]) {
				t.Fatalf("got %q does not preserve requested tail", got)
			}
		})
	}
}

func TestHeadTailTinyBudgetFallsBackToPrefix(t *testing.T) {
	got := HeadTailStrategy{HeadRatio: 0.5}.Truncate("abcdefghijklmnopqrstuvwxyz", 5)
	if got != "abcde" {
		t.Fatalf("HeadTailStrategy.Truncate() = %q, want abcde", got)
	}
}
