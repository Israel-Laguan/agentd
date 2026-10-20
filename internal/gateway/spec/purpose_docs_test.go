package spec

import (
	"os"
	"strings"
	"testing"
)

// The taxonomy table in docs/ai-usage-report.md must list every purpose in the
// closed set with its provisional level, so the doc cannot drift from the code.
func TestUsageReportDocListsEveryPurposeWithItsLevel(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/ai-usage-report.md")
	if err != nil {
		t.Fatalf("read the report doc: %v", err)
	}
	rows := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		cells := strings.Split(line, "|")
		if len(cells) > 3 && strings.HasPrefix(strings.TrimSpace(cells[1]), "`") {
			rows[strings.Trim(strings.TrimSpace(cells[1]), "`")] = strings.TrimSpace(cells[3])
		}
	}
	for _, p := range AllPurposes() {
		level, ok := rows[string(p)]
		if !ok {
			t.Errorf("docs/ai-usage-report.md has no row for %s", p)
			continue
		}
		if !strings.HasPrefix(level, string(p.MinLevel())) {
			t.Errorf("%s: doc level %q, code level %q", p, level, p.MinLevel())
		}
	}
}
