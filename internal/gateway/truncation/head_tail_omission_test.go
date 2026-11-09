package truncation

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestHeadTailReportsExactOmittedBytes(t *testing.T) {
	input := strings.Repeat("αstart\n", 30) + "end"
	got := (HeadTailStrategy{HeadRatio: .5}).Truncate(input, 80)
	if len(got) > 80 || !utf8.ValidString(got) {
		t.Fatalf("invalid output (%d bytes): %q", len(got), got)
	}
	var omitted int
	start := strings.Index(got, "【")
	end := strings.Index(got, " bytes omitted)")
	if start < 0 || end < 0 {
		t.Fatalf("missing omission marker: %q", got)
	}
	end += len(" bytes omitted)")
	if _, err := fmt.Sscanf(got[start:end], "【...】(%d bytes omitted)", &omitted); err != nil {
		t.Fatalf("bad omission marker: %q", got)
	}
	if omitted != len(input)-(len(got)-(end-start)) {
		t.Fatalf("incorrect omitted count: %q", got)
	}
}

func TestUTF8SafePrefixChecksOnlyTheEnd(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"a\xe4\xb8x", "a"},   // partial rune at the cut is dropped
		{"a中b", "a"},          // cut inside a valid rune backs off to it
		{"\xffabc", "\xffab"}, // an invalid byte earlier does not empty the prefix
		{"ab\x80cd", "ab"},    // a stray continuation byte at the end is dropped
	} {
		if got := utf8SafePrefix(tc.input, 3); got != tc.want {
			t.Errorf("utf8SafePrefix(%q, 3) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestHeadTailSmallBudgetKeepsBareMarker(t *testing.T) {
	got := HeadTailStrategy{HeadRatio: .5}.Truncate(strings.Repeat("a", 100), 20)
	if len(got) > 20 || !strings.Contains(got, truncationMarker) {
		t.Fatalf("Truncate = %q (%d bytes), want the bare marker within 20 bytes", got, len(got))
	}
}
