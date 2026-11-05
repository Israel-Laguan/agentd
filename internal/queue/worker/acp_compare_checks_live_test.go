//go:build live

package worker

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
)

// liveSeed is the word counter every run starts from (SP-022).
var liveSeed = map[string]string{
	"go.mod": "module wc\n\ngo 1.22\n",
	"main.go": `package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// CountWords returns the number of whitespace-separated words in text.
func CountWords(text string) int {
	return len(strings.Fields(text))
}

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(CountWords(string(data)))
}
`,
	"main_test.go": `package main

import "testing"

func TestCountWords(t *testing.T) {
	if got := CountWords("one two  three\n"); got != 3 {
		t.Fatalf("CountWords = %d, want 3", got)
	}
}
`,
}

type liveTask struct {
	name, title, description string
	ambiguous                bool
	// check is a host-side test of the behavior the task asked for.
	check string
}

const linesCheck = `
	for in, want := range map[string]int{"": 0, "one": 1, "one\n": 1, "a\nb": 2, "a\nb\n": 2, "\n": 1, "\n\n": 2} {
		if got := CountLines(in); got != want {
			t.Errorf("CountLines(%q) = %d, want %d", in, got, want)
		}
	}`

var allLiveTasks = []liveTask{
	{name: "small", title: "Add CountLines",
		description: "Add a function CountLines(text string) int to main.go that returns the number of lines in text, and a test for it in main_test.go. Keep go test passing.",
		check:       linesCheck},
	{name: "multi", title: "Split the counters and add flags",
		description: "Move CountWords into a new file count.go and add CountLines(text string) int and CountChars(text string) int there, with tests in count_test.go. Add -l (print the line count) and -c (print the character count) flags to the CLI in main.go. Keep go test passing.",
		check:       linesCheck + "\n\tif CountChars(\"hello\") != 5 || CountWords(\"a b\") != 2 {\n\t\tt.Error(\"CountChars or CountWords is wrong\")\n\t}"},
	{name: "ambiguous", title: "Change the output format", ambiguous: true,
		description: "Change the CLI output to the format the client asked for last week."},
}

func liveTasks(names []string) []liveTask {
	var out []liveTask
	for _, task := range allLiveTasks {
		if slices.Contains(names, task.name) {
			out = append(out, task)
		}
	}
	return out
}

func seedWorkspace(t *testing.T, dir string) {
	t.Helper()
	for name, content := range liveSeed {
		writeLive(t, filepath.Join(dir, name), content)
	}
}

func writeLive(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// liveResult is one run, checked on the host.
type liveResult struct {
	side, task string
	run        int
	state      models.TaskState
	goTest     bool     // go test ./... passes in the workspace
	behavior   bool     // the host's own test of the asked behavior passes
	wrong      []string // how the behavior test failed
	lost       []string // seed functions no longer defined anywhere
	changed    []string // seed files changed, and files added
	ambiguous  bool
	// refusedOther counts permission requests refused as a kind the driver
	// cannot check, such as agentd's own MCP tools.
	refusedOther int64
	latency      time.Duration
	events       []string // the worker's last events, for a failed run
}

// pass is the host's verdict: an ambiguous task must leave the source files
// as they were; any other must pass both tests and lose no seed function.
func (r liveResult) pass() bool {
	if r.ambiguous {
		return !slices.ContainsFunc(r.changed, func(f string) bool { return strings.HasSuffix(f, ".go") })
	}
	return r.goTest && r.behavior && len(r.lost) == 0
}

func checkWorkspace(t *testing.T, dir string, task liveTask) liveResult {
	t.Helper()
	r := liveResult{ambiguous: task.ambiguous, goTest: goTest(dir, "./...")}
	defined := map[string]bool{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if e.IsDir() || err != nil {
			continue
		}
		if seed, ok := liveSeed[e.Name()]; !ok || !bytes.Equal(data, []byte(seed)) {
			r.changed = append(r.changed, e.Name())
		}
		for _, fn := range funcs(e.Name(), data) {
			defined[fn] = true
		}
	}
	for _, name := range slices.Sorted(maps.Keys(liveSeed)) {
		for _, fn := range funcs(name, []byte(liveSeed[name])) {
			if !defined[fn] {
				r.lost = append(r.lost, fn)
			}
		}
	}
	if task.check != "" {
		copyDir := t.TempDir()
		_ = exec.Command("cp", "-a", dir+"/.", copyDir).Run()
		writeLive(t, filepath.Join(copyDir, "zz_agentd_check_test.go"), "package main\n\nimport \"testing\"\n\nfunc TestAgentdCheck(t *testing.T) {"+task.check+"\n}\n")
		r.behavior, r.wrong = goTestOutput(copyDir, "-run", "TestAgentdCheck", ".")
	}
	return r
}

func goTest(dir string, args ...string) bool {
	ok, _ := goTestOutput(dir, args...)
	return ok
}

// goTestOutput also returns the failure lines (Errorf output and build errors).
func goTestOutput(dir string, args ...string) (bool, []string) {
	cmd := exec.Command("go", append([]string{"test", "-count=1"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	var wrong []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); strings.Contains(line, "_test.go:") || strings.Contains(line, ".go:") && !strings.HasPrefix(line, "ok") {
			wrong = append(wrong, line)
		}
	}
	return err == nil, wrong
}

// funcs are the top-level functions a Go file defines.
func funcs(name string, src []byte) []string {
	if !strings.HasSuffix(name, ".go") {
		return nil
	}
	f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var out []string
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil {
			out = append(out, fn.Name.Name)
		}
	}
	return out
}

func liveTable(results []liveResult) string {
	var b strings.Builder
	b.WriteString("| Side | Task | Run | State | go test | Behavior | Seed functions lost | Files changed | MCP refusals | Latency | Pass |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, r := range results {
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %t | %t | %s | %s | %d | %s | %t |\n", r.side, r.task, r.run, r.state,
			r.goTest, r.behavior, strings.Join(r.lost, " "), strings.Join(r.changed, " "), r.refusedOther,
			r.latency.Round(time.Second), r.pass())
	}
	return b.String()
}
