package db

import (
	"testing"
	"time"
)

// TestFormatTime_LexicographicOrderIsChronological pins the fixed-width
// fraction: an exact-second timestamp and a fractional timestamp in the same
// second must sort chronologically under plain string comparison (SQLite
// range queries compare the stored TEXT).
func TestFormatTime_LexicographicOrderIsChronological(t *testing.T) {
	whole := time.Date(2026, 9, 20, 18, 0, 0, 0, time.UTC)
	fractional := whole.Add(123456789 * time.Nanosecond)
	wholeStr, fracStr := FormatTime(whole), FormatTime(fractional)
	if wholeStr >= fracStr {
		t.Fatalf("lexicographic order wrong: %q >= %q", wholeStr, fracStr)
	}
	if got := len(fracStr); got != len(wholeStr) {
		t.Fatalf("width varies: %q (%d) vs %q (%d)", wholeStr, len(wholeStr), fracStr, got)
	}
}

// TestFormatTime_ParseTimeRoundTrip ensures every FormatTime output parses
// back to the same instant, including exact seconds.
func TestFormatTime_ParseTimeRoundTrip(t *testing.T) {
	cases := []time.Time{
		time.Date(2026, 9, 20, 18, 0, 0, 0, time.UTC),
		time.Now().UTC(),
		time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC),
	}
	for _, tc := range cases {
		parsed, err := ParseTime(FormatTime(tc))
		if err != nil {
			t.Fatalf("ParseTime(FormatTime(%v)): %v", tc, err)
		}
		if !parsed.Equal(tc) {
			t.Fatalf("round trip mismatch: %v -> %q -> %v", tc, FormatTime(tc), parsed)
		}
	}
}

// TestNormalizeTimestampString covers legacy spellings: variable-width
// RFC3339Nano and the old SQLite default must rewrite to fixed width, while
// canonical values, tombstones, and garbage are untouched.
func TestNormalizeTimestampString(t *testing.T) {
	canonical := FormatTime(time.Date(2026, 9, 20, 18, 0, 0, 0, time.UTC))
	cases := []struct {
		in      string
		changed bool
		want    string
	}{
		{"2026-09-20T18:00:00Z", true, canonical},
		{"2026-09-20T18:00:00.123Z", true, "2026-09-20T18:00:00.123000000Z"},
		{"2006-01-02 15:04:05", true, "2006-01-02T15:04:05.000000000Z"},
		{canonical, false, canonical},
		{"CURATED:2026-09-20T18:00:00Z", false, "CURATED:2026-09-20T18:00:00Z"},
		{"", false, ""},
		{"not-a-timestamp", false, "not-a-timestamp"},
	}
	for _, tc := range cases {
		got, changed := NormalizeTimestampString(tc.in)
		if changed != tc.changed || got != tc.want {
			t.Errorf("NormalizeTimestampString(%q) = (%q, %v), want (%q, %v)",
				tc.in, got, changed, tc.want, tc.changed)
		}
	}
}
