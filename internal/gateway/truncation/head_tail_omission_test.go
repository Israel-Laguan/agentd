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
