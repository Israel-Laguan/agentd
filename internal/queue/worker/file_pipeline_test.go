package worker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"agentd/internal/gateway"
)

type fakeEmbedder struct {
	calls int32
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	atomic.AddInt32(&f.calls, 1)
	out := make([][]float32, len(texts))
	for i, text := range texts {
		vec := make([]float32, 4)
		if strings.Contains(text, "topic") {
			vec[0] = 1
			vec[1] = 1
		} else if strings.Contains(text, "alpha") {
			vec[0] = 1
			vec[2] = 1
		} else {
			vec[3] = 1
		}
		vec[0] += float32(i) * 0.001
		out[i] = vec
	}
	return out, nil
}

type countingConverter struct {
	calls int32
}

func (c *countingConverter) convert(_ context.Context, fullPath string, _ []byte) (string, error) {
	atomic.AddInt32(&c.calls, 1)
	return "converted:" + filepath.Base(fullPath), nil
}

func TestFileConverter_PDFUsesPdftotextOrMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.4 fake"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := defaultConvert(context.Background(), pdfPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "%PDF") {
		t.Fatalf("expected markdown, got raw pdf bytes: %q", out)
	}
	if _, err := exec.LookPath("pdftotext"); err != nil {
		if out != unsupportedPDFMarker {
			t.Fatalf("expected unsupported marker without pdftotext, got %q", out)
		}
	}
}

func TestFilePipeline_ProcessRead_CacheHit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := NewDocStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	rel := "note.txt"
	if err := os.WriteFile(filepath.Join(workspace, rel), []byte("hello cache"), 0644); err != nil {
		t.Fatal(err)
	}
	counter := &countingConverter{}
	pipe := NewFilePipeline(FilePipelineConfig{
		Workspace: workspace,
		Store:     store,
		Converter: NewFileConverterWith(counter.convert),
		Embedder:  &fakeEmbedder{},
		TopK:      5,
	})
	info, err := os.Stat(filepath.Join(workspace, rel))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, rel))
	if err != nil {
		t.Fatal(err)
	}
	first, err := pipe.ProcessRead(context.Background(), rel, raw, info)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "converted:note.txt") {
		t.Fatalf("first read: %q", first)
	}
	if atomic.LoadInt32(&counter.calls) != 1 {
		t.Fatalf("converter calls = %d, want 1", counter.calls)
	}
	second, err := pipe.ProcessRead(context.Background(), rel, raw, info)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("cache miss: first=%q second=%q", first, second)
	}
	if atomic.LoadInt32(&counter.calls) != 1 {
		t.Fatalf("converter calls after cache = %d, want 1", counter.calls)
	}
}

func TestFileSelector_TopK(t *testing.T) {
	t.Parallel()
	embedder := &fakeEmbedder{}
	sel := NewFileSelector(embedder, 2)
	docs := make([]*CachedDoc, 10)
	for i := range docs {
		docs[i] = &CachedDoc{
			Path:     filepath.Join("docs", "file"+string(rune('a'+i))+".md"),
			Markdown: strings.Repeat("alpha ", 20-i) + strings.Repeat("noise ", i),
		}
	}
	taskQuery := strings.Repeat("alpha ", 30)
	selected, err := sel.Select(context.Background(), taskQuery, docs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 {
		t.Fatalf("selected %d docs, want 2", len(selected))
	}
	if !strings.Contains(selected[0].Path, "filea.md") {
		t.Fatalf("expected filea first, got %s", selected[0].Path)
	}
}

func TestFileSelector_PinnedAlwaysIncluded(t *testing.T) {
	t.Parallel()
	embedder := &fakeEmbedder{}
	sel := NewFileSelector(embedder, 2)
	docs := make([]*CachedDoc, 5)
	for i := range docs {
		docs[i] = &CachedDoc{
			Path:     filepath.Join("docs", "f"+string(rune('0'+i))+".md"),
			Markdown: strings.Repeat("noise ", 50),
		}
	}
	pinnedPath := filepath.Join("docs", "pinned.md")
	docs = append(docs, &CachedDoc{Path: pinnedPath, Markdown: strings.Repeat("noise ", 50)})
	pinned := map[string]struct{}{normalizePathKey(pinnedPath): {}}
	selected, err := sel.Select(context.Background(), strings.Repeat("alpha ", 30), docs, pinned)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range selected {
		if d.Path == pinnedPath {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("pinned path %q not in selection: %v", pinnedPath, selected)
	}
}

func TestFilePipeline_Process_TopKInjection(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	store, err := NewDocStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"a.md", "b.md", "c.md", "d.md", "e.md", "f.md"}
	for i, rel := range paths {
		var content string
		if i <= 2 {
			content = strings.Repeat("topic ", 30) + string(rune('A'+i))
		} else {
			content = strings.Repeat("other ", 30) + string(rune('X'+i))
		}
		if err := os.WriteFile(filepath.Join(workspace, rel), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	pipe := NewFilePipeline(FilePipelineConfig{
		Workspace: workspace,
		Store:     store,
		Embedder:  &fakeEmbedder{},
		TopK:      3,
		TaskQuery: strings.Repeat("topic ", 40),
	})
	out, err := pipe.Process(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}
	count := strings.Count(out, "# file:")
	if count != 3 {
		t.Fatalf("injected %d files, want 3; output:\n%s", count, out)
	}
}

func TestParsePinnedPaths(t *testing.T) {
	t.Parallel()
	q := "do something\n\n[agentd file reference]\nname: spec.pdf\npath: uploads/spec.pdf\n"
	got := ParsePinnedPaths(q)
	if len(got) != 1 || got[0] != "uploads/spec.pdf" {
		t.Fatalf("got %v", got)
	}
}

func TestToolExecutor_Read_WithPipeline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte("# Hi"), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := NewDocStore(filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	ex := NewToolExecutor(nil, dir, nil, 0)
	ex.filePipeline = NewFilePipeline(FilePipelineConfig{
		Workspace: dir,
		Store:     store,
		Converter: NewFileConverter(),
		TopK:      5,
	})
	out := ex.Execute(context.Background(), gatewayToolCallRead("hello.md"))
	if strings.Contains(out, toolErrorPrefix) {
		t.Fatalf("unexpected error: %s", out)
	}
	if out != "# Hi" {
		t.Fatalf("got %q", out)
	}
}

func gatewayToolCallRead(path string) gateway.ToolCall {
	return gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameRead,
			Arguments: `{"path": "` + path + `"}`,
		},
	}
}

// Ensure os.FileInfo mtime used in cache invalidation.
func TestDocStore_InvalidatesOnMtimeChange(t *testing.T) {
	t.Parallel()
	store, err := NewDocStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	doc := &CachedDoc{
		ContentHash: "abc",
		Path:        "f.txt",
		Markdown:    "v1",
		SourceSize:  10,
		SourceMtime: 1,
	}
	if err := store.Put(doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("abc", 10, 1); !ok {
		t.Fatal("expected cache hit")
	}
	if _, ok := store.Get("abc", 10, 2); ok {
		t.Fatal("expected cache miss after mtime change")
	}
}

