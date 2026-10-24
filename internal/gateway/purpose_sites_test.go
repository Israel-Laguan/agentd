package gateway_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// wantSites pins SP-023's mapping: every place that builds an AIRequest or an
// EmbedRequest outside the provider adapters, and the Purpose each one sends.
// A new site fails this test until it is added here with a purpose.
var wantSites = map[string][]string{
	"internal/gateway/routing/intent.go":                 {"PurposeIntakeClassifyIntent"},
	"internal/gateway/routing/scope.go":                  {"PurposeIntakeAnalyzeScope"},
	"internal/gateway/routing/router_plan.go":            {"planPurpose"},
	"internal/gateway/truncation/truncator_summarize.go": {"PurposeHiddenTruncateSummary"},
	"internal/agent/runtime/topic_guard.go":              {"PurposeOverheadTopicDrift"},
	"internal/agent/context/context_summarize.go":        {"PurposeOverheadCompactContext"},
	"internal/agent/subagent/subagent.go":                {"PurposeSubagentTurn"},
	"internal/agent/filecontext/file_embedder.go":        {"PurposeOverheadEmbedFiles"},
	"internal/queue/worker/worker_permission.go":         {"PurposeExecPermissionAlt"},
	"internal/queue/worker/targeted_redo.go":             {"PurposeExecRepairSection", "PurposeExecRespecTurn"},
	"internal/queue/worker/phase_splitter.go":            {"PurposeExecWorkPlan"},
	"internal/queue/worker/worker_legacy.go":             {"PurposeExecLegacyCommand"},
	"internal/queue/worker/worker_batch.go":              {"PurposeExecBatch"},
	"internal/queue/worker/task_runner.go":               {"PurposeExecSuggestCommand"},
	"internal/queue/worker/agentic/handlers.go":          {"PurposeExecTurn"},
	"internal/queue/worker/elicitor.go":                  {"PurposeCreateElicit"},
	"internal/queue/worker/healing_split.go":             {"PurposeCreateSplitTask"},
	"internal/memory/librarian_summarize.go":             {"PurposeMaintExtractMemory", "PurposeMaintSummarizeLog", "PurposeMaintSummarizeLog"},
	"internal/memory/dream.go":                           {"PurposeMaintMergeMemories"},
	"internal/config/health_warmup.go":                   {"PurposeSystemWarmup"},
}

func TestEveryCallSiteSendsItsPurpose(t *testing.T) {
	root := filepath.Join("..", "..")
	got := map[string][]string{}
	for _, dir := range []string{"internal", "cmd"} {
		walkSites(t, root, dir, got)
	}
	for file, want := range wantSites {
		have := got[file]
		sort.Strings(have)
		if strings.Join(have, ",") != strings.Join(want, ",") {
			t.Errorf("%s: purposes %v, want %v", file, have, want)
		}
		delete(got, file)
	}
	for file, have := range got {
		t.Errorf("%s: unexpected call site sending %v; add it to wantSites with a purpose", file, have)
	}
}

func walkSites(t *testing.T, root, dir string, got map[string][]string) {
	t.Helper()
	err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "internal/gateway/providers/") {
			return nil
		}
		file, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if lit, ok := n.(*ast.CompositeLit); ok && isRequestType(lit.Type) {
				got[rel] = append(got[rel], purposeOf(lit))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func isRequestType(e ast.Expr) bool {
	name := ""
	switch x := e.(type) {
	case *ast.Ident:
		name = x.Name
	case *ast.SelectorExpr:
		name = x.Sel.Name
	}
	return name == "AIRequest" || name == "EmbedRequest"
}

// purposeOf returns the name used as the literal's Purpose ("" when missing or
// a string literal such as "").
func purposeOf(lit *ast.CompositeLit) string {
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok || kv.Key.(*ast.Ident).Name != "Purpose" {
			continue
		}
		switch v := kv.Value.(type) {
		case *ast.SelectorExpr:
			return v.Sel.Name
		case *ast.Ident:
			return v.Name
		case *ast.CallExpr:
			if id, ok := v.Fun.(*ast.Ident); ok {
				return id.Name
			}
		}
	}
	return ""
}
