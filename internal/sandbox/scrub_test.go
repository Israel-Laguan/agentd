package sandbox

import (
	"strings"
	"testing"
)

func TestScrubberMasksKnownPattern(t *testing.T) {
	s := NewScrubber(nil)
	got := s.Scrub("token=sk-1234567890123456789012345678901234567890")
	if strings.Contains(got, "sk-123456") {
		t.Fatalf("Scrub() leaked secret: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("Scrub() = %q, want [REDACTED]", got)
	}
}

func TestScrubberHonorsCustomPattern(t *testing.T) {
	s := NewScrubber([]string{`custom-secret-[A-Za-z0-9]+`})
	got := s.Scrub("custom-secret-abc123")
	if got != "[REDACTED]" {
		t.Fatalf("Scrub() = %q, want %q", got, "[REDACTED]")
	}
}

func TestScrubberLeavesNormalLineUntouched(t *testing.T) {
	s := NewScrubber(nil)
	const input = "build complete"
	if got := s.Scrub(input); got != input {
		t.Fatalf("Scrub() = %q, want %q", got, input)
	}
}
