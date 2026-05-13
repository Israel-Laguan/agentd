package truncation

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMiddleOutShortInput(t *testing.T) {
	got := MiddleOut("short", 20)
	if got != "short" {
		t.Fatalf("MiddleOut() = %q, want short", got)
	}
}

func TestMiddleOutLongInput(t *testing.T) {
	got := MiddleOut("abcdefghijklmnopqrstuvwxyz", 20)
	want := "abcdefg【...】stuvwxyz"
	if got != want {
		t.Fatalf("MiddleOut() = %q, want %q", got, want)
	}
}

func TestMiddleOutAcceptanceLargeContext(t *testing.T) {
	input := strings.Repeat("a", 25000) + strings.Repeat("b", 25000)
	got := MiddleOut(input, 10000)
	if utf8.RuneCountInString(got) > 10000 {
		t.Fatalf("utf8.RuneCountInString(MiddleOut()) = %d, want <= 10000", utf8.RuneCountInString(got))
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

func TestMiddleOutUTF8SafeAndCharacterBounded(t *testing.T) {
	input := "áéíóú世界abcdefghijklmnopqrstuvwxyz"
	got := MiddleOut(input, 12)

	if !utf8.ValidString(got) {
		t.Fatalf("MiddleOut() produced invalid UTF-8: %q", got)
	}
	if utf8.RuneCountInString(got) > 12 {
		t.Fatalf("MiddleOut() rune count = %d, want <= 12", utf8.RuneCountInString(got))
	}
	if !strings.Contains(got, truncationMarker) {
		t.Fatalf("MiddleOut() missing marker %q", truncationMarker)
	}
}
