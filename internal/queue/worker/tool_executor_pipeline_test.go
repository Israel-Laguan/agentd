package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/gateway"

	wfilecontext "agentd/internal/agent/filecontext"
	agenttools "agentd/internal/agent/tools"
)

func TestToolExecutor_Read_PipelineFallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	content := "raw-fallback-content"
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	ex := agenttools.NewToolExecutor(nil, dir, nil, 0)
	ex.SetFilePipeline(wfilecontext.NewFilePipeline(wfilecontext.FilePipelineConfig{
		Workspace: dir,
		Converter: wfilecontext.NewFileConverterWith(failingConvertFunc),
		TopK:      5,
	}))
	out := ex.Execute(context.Background(), gatewayToolCallForRead("note.txt"))
	if strings.Contains(out, agenttools.ToolErrorPrefix) {
		t.Fatalf("expected raw fallback, got error: %s", out)
	}
	if out != content {
		t.Fatalf("got %q, want %q", out, content)
	}
}

func TestToolExecutor_Read_WithPipeline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte("# Hi"), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := wfilecontext.NewDocStore(filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	ex := agenttools.NewToolExecutor(nil, dir, nil, 0)
	ex.SetFilePipeline(wfilecontext.NewFilePipeline(wfilecontext.FilePipelineConfig{
		Workspace: dir,
		Store:     store,
		Converter: wfilecontext.NewFileConverter(),
		TopK:      5,
	}))
	out := ex.Execute(context.Background(), gatewayToolCallForRead("hello.md"))
	if strings.Contains(out, agenttools.ToolErrorPrefix) {
		t.Fatalf("unexpected error: %s", out)
	}
	if out != "# Hi" {
		t.Fatalf("got %q", out)
	}
}

func gatewayToolCallForRead(path string) gateway.ToolCall {
	return gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      agenttools.ToolNameRead,
			Arguments: `{"path": "` + path + `"}`,
		},
	}
}

func failingConvertFunc(_ context.Context, _ string, _ []byte) (string, error) {
	return "", os.ErrInvalid
}
