package ci_test

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// run executes pr_size.sh and returns its exit code and combined output.
func run(t *testing.T, args ...string) (int, string) {
	t.Helper()
	out, err := exec.Command("bash", append([]string{"pr_size.sh"}, args...)...).CombinedOutput()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &ee):
		return ee.ExitCode(), string(out)
	default:
		t.Fatal(err)
		return -1, ""
	}
}

func TestPRSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want int
		msg  string
	}{
		{"well under limits", []string{"5", "100", "20", ""}, 0, ""},
		{"99 files passes", []string{"99", "10", "10", ""}, 0, ""},
		{"100 files fails", []string{"100", "10", "10", ""}, 1, "files"},
		{"1000 changed lines passes", []string{"10", "600", "400", ""}, 0, ""},
		{"1001 changed lines fails", []string{"10", "600", "401", ""}, 1, "lines"},
		{"deletions count toward lines", []string{"10", "0", "1001", ""}, 1, "lines"},
		{"additions count toward lines", []string{"10", "1001", "0", ""}, 1, "lines"},
		{"label overrides both limits", []string{"150", "5000", "600", "bug,large-pr-ok"}, 0, "large-pr-ok"},
		{"similar label does not override", []string{"150", "10", "10", "large-pr-okay,not-large-pr-ok"}, 1, "files"},
		{"label absent from list fails", []string{"150", "10", "10", "bug"}, 1, "files"},
		{"too few args is a usage error", []string{"1", "2"}, 2, "usage"},
		{"non-numeric input is a usage error", []string{"x", "1", "1", ""}, 2, "usage"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			code, out := run(t, tc.args...)
			if code != tc.want {
				t.Fatalf("exit = %d, want %d\n%s", code, tc.want, out)
			}
			if !strings.Contains(out, tc.msg) {
				t.Fatalf("output missing %q:\n%s", tc.msg, out)
			}
		})
	}
}
