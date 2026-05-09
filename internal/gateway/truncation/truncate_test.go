package truncation

import (
	"strings"
	"testing"
)

func TestMiddleOutShortInput(t *testing.T) {
	got := MiddleOut("short", 20)
	if got != "short" {
		t.Fatalf("MiddleOut() = %q, want short", got)
	}
}

func TestMiddleOutLongInput(t *testing.T) {
	got := MiddleOut("abcdefghijklmnopqrstuvwxyz", 20)
	want := "abcdefghijklmnopqrst"
	if got != want {
		t.Fatalf("MiddleOut() = %q, want %q", got, want)
	}
}

func TestMiddleOutAcceptanceLargeContext(t *testing.T) {
	input := strings.Repeat("a", 25000) + strings.Repeat("b", 25000)
	got := MiddleOut(input, 10000)
	if len(got) > 10000 {
		t.Fatalf("len(MiddleOut()) = %d, want <= 10000", len(got))
	}
	if !strings.Contains(got, truncationMarker) {
		t.Fatalf("MiddleOut() missing marker %q", truncationMarker)
	}
	if got[:10] != input[:10] {
		t.Fatalf("MiddleOut() does not preserve head")
	}
	if got[len(got)-10:] != input[len(input)-10:] {
		t.Fatalf("MiddleOut() does not preserve tail")
	}
}
