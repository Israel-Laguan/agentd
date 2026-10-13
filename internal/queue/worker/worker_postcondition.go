package worker

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"agentd/internal/models"
)

// criterionFilePattern finds workspace-file mentions inside free-text success
// criteria (e.g. "main.go parses", "wire up internal/api/server.go").
var criterionFilePattern = regexp.MustCompile(`[A-Za-z0-9_][A-Za-z0-9_./-]*\.[A-Za-z0-9]+`)

// criterionFileExtensions whitelists the file types the post-condition check
// enforces. Anything else (notably URL hosts like example.com) is skipped so
// prose never fails a task.
var criterionFileExtensions = map[string]struct{}{
	".go": {}, ".js": {}, ".jsx": {}, ".ts": {}, ".tsx": {}, ".py": {},
	".rb": {}, ".java": {}, ".c": {}, ".h": {}, ".cpp": {}, ".rs": {},
	".sh": {}, ".bash": {}, ".sql": {}, ".md": {}, ".yaml": {}, ".yml": {},
	".json": {}, ".toml": {}, ".html": {}, ".css": {},
}

// criterionFiles extracts deduplicated workspace-file mentions from success
// criteria, in first-seen order.
func criterionFiles(criteria []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, criterion := range criteria {
		for _, token := range criterionFilePattern.FindAllString(criterion, -1) {
			clean := strings.Trim(token, "./")
			if _, ok := seen[clean]; ok {
				continue
			}
			if _, ok := criterionFileExtensions[strings.ToLower(filepath.Ext(clean))]; !ok {
				continue
			}
			seen[clean] = struct{}{}
			out = append(out, clean)
		}
	}
	return out
}

// verifyPostConditions checks the cheap post-conditions derivable from a
// task's success criteria after an exit-0 command (B-039): every named file
// must exist and be non-empty, and named Go files must still parse. It is
// read-only (stat + parse, no execution) and fails closed toward nil —
// unresolvable mentions are skipped, never failed — so prose-heavy criteria
// cannot wrongly fail a task.
func verifyPostConditions(task models.Task, workspacePath string) error {
	for _, name := range criterionFiles(task.SuccessCriteria) {
		resolved := filepath.Join(workspacePath, filepath.FromSlash(name))
		info, err := os.Stat(resolved)
		if err != nil {
			return fmt.Errorf("post-condition check failed: criterion names %q, but it does not exist in the workspace", name)
		}
		if info.IsDir() {
			continue
		}
		if info.Size() == 0 {
			return fmt.Errorf("post-condition check failed: criterion file %q is empty", name)
		}
		if strings.EqualFold(filepath.Ext(name), ".go") {
			if _, err := parser.ParseFile(token.NewFileSet(), resolved, nil, 0); err != nil {
				return fmt.Errorf("post-condition check failed: criterion file %q no longer parses: %v", name, err)
			}
		}
	}
	return nil
}
