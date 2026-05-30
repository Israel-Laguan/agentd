package wfilecontext

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"agentd/internal/paths"
)

// FilePipeline orchestrates convert → cache → embed → select.
type FilePipeline struct {
	workspace string
	converter *FileConverter
	store     *DocStore
	selector  *FileSelector
	embedder  Embedder
	topK      int
	taskQuery string
	pinned    map[string]struct{}
}

type FilePipelineConfig struct {
	Workspace string
	Converter *FileConverter
	Store     *DocStore
	Embedder  Embedder
	TopK      int
	TaskQuery string
	Pinned    []string
}

func NewFilePipeline(cfg FilePipelineConfig) *FilePipeline {
	converter := cfg.Converter
	if converter == nil {
		converter = NewFileConverter()
	}
	topK := cfg.TopK
	if topK <= 0 {
		topK = 5
	}
	pinned := make(map[string]struct{}, len(cfg.Pinned))
	for _, p := range cfg.Pinned {
		pinned[normalizePathKey(p)] = struct{}{}
	}
	return &FilePipeline{
		workspace: cfg.Workspace,
		converter: converter,
		store:     cfg.Store,
		embedder:  cfg.Embedder,
		selector:  NewFileSelector(cfg.Embedder, topK),
		topK:      topK,
		taskQuery: cfg.TaskQuery,
		pinned:    pinned,
	}
}

// ProcessRead converts and caches a single file without embedding. Selection
// (topK) is skipped because the read tool always returns the requested file's
// content; embeddings are computed later during batch Process/Select if needed.
func (p *FilePipeline) ProcessRead(ctx context.Context, relPath, resolvedPath string, raw []byte, info os.FileInfo) (string, error) {
	doc, err := p.ingest(ctx, relPath, resolvedPath, raw, info, false)
	if err != nil {
		return "", err
	}
	return doc.Markdown, nil
}

func (p *FilePipeline) Process(ctx context.Context, relPaths []string) (string, error) {
	docs := make([]*CachedDoc, 0, len(relPaths))
	for _, rel := range relPaths {
		full, err := paths.ResolveWorkspaceFile(p.workspace, rel)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(full)
		if err != nil {
			return "", err
		}
		raw, err := os.ReadFile(full)
		if err != nil {
			return "", err
		}
		doc, err := p.ingest(ctx, rel, full, raw, info, true)
		if err != nil {
			return "", err
		}
		docs = append(docs, doc)
	}
	selected, err := p.selector.Select(ctx, p.taskQuery, docs, p.pinned)
	if err != nil {
		return "", err
	}
	return formatDocsForInjection(selected), nil
}

func (p *FilePipeline) ingest(ctx context.Context, relPath, resolvedPath string, raw []byte, info os.FileInfo, embed bool) (*CachedDoc, error) {
	hash := contentHash(raw)
	size := info.Size()
	mtime := info.ModTime().Unix()

	if doc, ok := p.ingestFromCache(ctx, relPath, hash, size, mtime, embed); ok {
		return doc, nil
	}

	convertPath := resolvedPath
	if convertPath == "" {
		convertPath = filepath.Join(p.workspace, relPath)
	}
	markdown, err := p.converter.convert(ctx, convertPath, raw)
	if err != nil {
		return nil, err
	}

	var embedding []float32
	if embed {
		embedding = p.embedMarkdown(ctx, relPath, markdown)
	}

	doc := &CachedDoc{
		ContentHash: hash,
		Path:        relPath,
		Markdown:    markdown,
		Embedding:   embedding,
		TokenCount:  estimateTokenCount(markdown),
		SourceSize:  size,
		SourceMtime: mtime,
	}

	if p.store != nil {
		if err := p.store.Put(doc); err != nil {
			slog.Warn("doc store put failed", "path", relPath, "error", err)
		}
	}
	return doc, nil
}

func (p *FilePipeline) ingestFromCache(ctx context.Context, relPath, hash string, size, mtime int64, embed bool) (*CachedDoc, bool) {
	if p.store == nil {
		return nil, false
	}
	cached, ok := p.store.Get(hash, size, mtime)
	if !ok {
		return nil, false
	}
	doc := *cached
	doc.Path = relPath
	p.backfillEmbedding(ctx, relPath, &doc, embed)
	return &doc, true
}

func (p *FilePipeline) backfillEmbedding(ctx context.Context, relPath string, doc *CachedDoc, embed bool) {
	if !embed || len(doc.Embedding) > 0 {
		return
	}
	embedding := p.embedMarkdown(ctx, relPath, doc.Markdown)
	if embedding == nil {
		return
	}
	doc.Embedding = embedding
	if p.store != nil {
		if err := p.store.Put(doc); err != nil {
			slog.Warn("doc store put failed", "path", relPath, "error", err)
		}
	}
}

func (p *FilePipeline) embedMarkdown(ctx context.Context, relPath, markdown string) []float32 {
	if p.embedder == nil {
		return nil
	}
	snippet := firstNTokens(markdown, embedSnippetTokens)
	vecs, err := p.embedder.Embed(ctx, []string{snippet})
	if err != nil {
		slog.Warn("file pipeline embed failed", "path", relPath, "error", err)
		return nil
	}
	if len(vecs) == 0 {
		return nil
	}
	return vecs[0]
}

func formatDocsForInjection(docs []*CachedDoc) string {
	var b strings.Builder
	for i, d := range docs {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("# file: ")
		b.WriteString(d.Path)
		b.WriteString("\n")
		b.WriteString(d.Markdown)
	}
	return b.String()
}

func contentHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func estimateTokenCount(text string) int {
	n := len(text) / 4
	if n < 1 {
		return 1
	}
	return n
}

func firstNTokens(text string, n int) string {
	if n <= 0 {
		return ""
	}
	words := strings.Fields(text)
	if len(words) <= n {
		return text
	}
	return strings.Join(words[:n], " ")
}

// ParsePinnedPaths extracts workspace-relative paths from agentd file reference blocks.
func ParsePinnedPaths(taskQuery string) []string {
	var result []string
	lines := strings.Split(taskQuery, "\n")
	inRef := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.Contains(trim, "[agentd file reference]") {
			inRef = true
			continue
		}
		if inRef && strings.HasPrefix(trim, "path:") {
			p := strings.TrimSpace(strings.TrimPrefix(trim, "path:"))
			if p != "" {
				result = append(result, p)
			}
		}
		if inRef && trim == "" {
			inRef = false
		}
	}
	return result
}

// ResolveWorkspaceFile is kept for backwards compatibility with tests.
func resolveWorkspaceFile(workspacePath, rel string) (string, error) {
	return paths.ResolveWorkspaceFile(workspacePath, rel)
}
