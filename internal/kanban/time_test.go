package kanban

import (
	"strings"
	"testing"
	"time"
)

func TestParseTime_RFC3339Nano(t *testing.T) {
	input := "2024-01-15T10:30:00.123456789Z"
	got, err := parseTime(input)
	if err != nil {
		t.Fatalf("parseTime() error = %v", err)
	}
	want, _ := time.Parse(time.RFC3339Nano, input)
	if !got.Equal(want.UTC()) {
		t.Fatalf("parseTime() = %v, want %v", got, want.UTC())
	}
}

func TestParseTime_LegacyFormat(t *testing.T) {
	got, err := parseTime("2024-01-15 10:30:00")
	if err != nil {
		t.Fatalf("parseTime() error = %v", err)
	}
	want := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("parseTime() = %v, want %v", got, want)
	}
}

func TestParseTime_InvalidPreservesRFC3339Error(t *testing.T) {
	_, err := parseTime("not-a-time")
	if err == nil {
		t.Fatal("parseTime() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "not-a-time") {
		t.Fatalf("error = %q, want quoted value in message", err)
	}
	_, rfcErr := time.Parse(time.RFC3339Nano, "not-a-time")
	if !strings.Contains(err.Error(), rfcErr.Error()) {
		t.Fatalf("error = %q, want RFC3339 parse detail %q", err, rfcErr)
	}
}
