package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
)

const (
	unsupportedPDFMarker  = "<!-- agentd: unsupported format (pdf conversion failed) -->"
	unsupportedPPTXMarker = "<!-- agentd: unsupported format (pptx) -->"
	unsupportedImageMarker = "<!-- agentd: unsupported format (image) -->"
	embedSnippetTokens    = 500
)

// Embedder produces dense vectors for relevance scoring.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// GatewayEmbedder adapts gateway.AIGateway embedding calls.
type GatewayEmbedder struct {
	Gateway gateway.AIGateway
	Model   string
}

func (g *GatewayEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if g.Gateway == nil {
		return nil, errors.New("gateway is nil")
	}
	resp, err := g.Gateway.Embed(ctx, spec.EmbedRequest{
		Input: texts,
		Model: g.Model,
	})
	if err != nil {
		return nil, err
	}
	return resp.Vectors, nil
}

// CachedDoc is a processed workspace document stored on disk.
type CachedDoc struct {
	ContentHash string    `json:"content_hash"`
	Path        string    `json:"path"`
	Markdown    string    `json:"markdown"`
	Embedding   []float32 `json:"embedding"`
	TokenCount  int       `json:"token_count"`
	SourceSize  int64     `json:"source_size"`
	SourceMtime int64     `json:"source_mtime"`
}

// DocStore persists cached documents keyed by content hash.
type DocStore struct {
	dir string
	mu  sync.Mutex
}

func NewDocStore(dir string) (*DocStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create doc store: %w", err)
	}
	return &DocStore{dir: dir}, nil
}

func (s *DocStore) cachePath(hash string) string {
	return filepath.Join(s.dir, hash+".json")
}

func (s *DocStore) Get(hash string, size int64, mtime int64) (*CachedDoc, bool) {
	path := s.cachePath(hash)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var doc CachedDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, false
	}
	if doc.SourceSize != size || doc.SourceMtime != mtime {
		return nil, false
	}
	return &doc, true
}

func (s *DocStore) Put(doc *CachedDoc) error {
	if doc == nil || doc.ContentHash == "" {
		return errors.New("invalid cached doc")
	}
	path := s.cachePath(doc.ContentHash)
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("open cache file: %w", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock cache file: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck

	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}

// ConverterFunc converts a file at fullPath to markdown.
type ConverterFunc func(ctx context.Context, fullPath string, raw []byte) (string, error)

// FileConverter turns workspace files into markdown.
type FileConverter struct {
	convert ConverterFunc
}

func NewFileConverter() *FileConverter {
	return &FileConverter{convert: defaultConvert}
}

func NewFileConverterWith(fn ConverterFunc) *FileConverter {
	if fn == nil {
		fn = defaultConvert
	}
	return &FileConverter{convert: fn}
}

func defaultConvert(ctx context.Context, fullPath string, _ []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(fullPath))
	switch ext {
	case ".pdf":
		return convertPDF(ctx, fullPath)
	case ".pptx", ".ppt":
		return unsupportedPPTXMarker, nil
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tiff", ".tif":
		return unsupportedImageMarker, nil
	default:
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
}

func convertPDF(ctx context.Context, fullPath string) (string, error) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return unsupportedPDFMarker, nil
	}
	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", fullPath, "-")
	out, err := cmd.Output()
	if err != nil {
		return unsupportedPDFMarker, nil
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return unsupportedPDFMarker, nil
	}
	return text, nil
}

// FileSelector ranks documents by embedding similarity to the task query.
type FileSelector struct {
	embedder Embedder
	topK     int
}

func NewFileSelector(embedder Embedder, topK int) *FileSelector {
	if topK <= 0 {
		topK = 5
	}
	return &FileSelector{embedder: embedder, topK: topK}
}

type scoredDoc struct {
	doc   *CachedDoc
	score float64
}

func (s *FileSelector) Select(ctx context.Context, taskQuery string, docs []*CachedDoc, pinned map[string]struct{}) ([]*CachedDoc, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	if len(docs) <= s.topK && s.embedder == nil {
		return docs, nil
	}

	var queryVec []float32
	var docVecs [][]float32

	if s.embedder != nil && strings.TrimSpace(taskQuery) != "" {
		texts := make([]string, 0, len(docs)+1)
		texts = append(texts, taskQuery)
		for _, d := range docs {
			texts = append(texts, firstNTokens(d.Markdown, embedSnippetTokens))
		}
		vecs, embedErr := s.embedder.Embed(ctx, texts)
		if embedErr != nil {
			slog.Warn("file selector embedding failed, using path order", "error", embedErr)
		} else if len(vecs) == len(texts) {
			queryVec = vecs[0]
			docVecs = vecs[1:]
		}
	}

	scored := make([]scoredDoc, 0, len(docs))
	for i, d := range docs {
		score := 0.0
		if queryVec != nil && i < len(docVecs) {
			score = cosineSimilarityFloat32(queryVec, docVecs[i])
		}
		if pinned != nil {
			if _, ok := pinned[normalizePathKey(d.Path)]; ok {
				score = 2.0
			}
		}
		scored = append(scored, scoredDoc{doc: d, score: score})
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].doc.Path < scored[j].doc.Path
	})

	limit := s.topK
	if limit > len(scored) {
		limit = len(scored)
	}
	out := make([]*CachedDoc, 0, limit)
	seen := make(map[string]struct{})
	for _, item := range scored {
		if len(out) >= limit {
			break
		}
		key := normalizePathKey(item.doc.Path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item.doc)
	}
	return out, nil
}

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
	Workspace  string
	Converter  *FileConverter
	Store      *DocStore
	Embedder   Embedder
	TopK       int
	TaskQuery  string
	Pinned     []string
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

func (p *FilePipeline) ProcessRead(ctx context.Context, relPath string, raw []byte, info os.FileInfo) (string, error) {
	doc, err := p.ingest(ctx, relPath, raw, info)
	if err != nil {
		return "", err
	}
	return doc.Markdown, nil
}

func (p *FilePipeline) Process(ctx context.Context, relPaths []string) (string, error) {
	docs := make([]*CachedDoc, 0, len(relPaths))
	for _, rel := range relPaths {
		full, err := filepath.Abs(filepath.Join(p.workspace, rel))
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
		doc, err := p.ingest(ctx, rel, raw, info)
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

func (p *FilePipeline) ingest(ctx context.Context, relPath string, raw []byte, info os.FileInfo) (*CachedDoc, error) {
	hash := contentHash(raw)
	size := info.Size()
	mtime := info.ModTime().Unix()

	if p.store != nil {
		if cached, ok := p.store.Get(hash, size, mtime); ok {
			return cached, nil
		}
	}

	fullPath := filepath.Join(p.workspace, relPath)
	markdown, err := p.converter.convert(ctx, fullPath, raw)
	if err != nil {
		return nil, err
	}

	var embedding []float32
	if p.embedder != nil {
		snippet := firstNTokens(markdown, embedSnippetTokens)
		vecs, embedErr := p.embedder.Embed(ctx, []string{snippet})
		if embedErr != nil {
			slog.Warn("file pipeline embed failed", "path", relPath, "error", embedErr)
		} else if len(vecs) > 0 {
			embedding = vecs[0]
		}
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

func cosineSimilarityFloat32(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, normA, normB float64
	for i := 0; i < n; i++ {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (sqrt64(normA) * sqrt64(normB))
}

func sqrt64(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 10; i++ {
		z -= (z*z - x) / (2 * z)
	}
	return z
}

func normalizePathKey(p string) string {
	return filepath.ToSlash(filepath.Clean(p))
}

// ParsePinnedPaths extracts workspace-relative paths from agentd file reference blocks.
func ParsePinnedPaths(taskQuery string) []string {
	var paths []string
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
				paths = append(paths, p)
			}
		}
		if inRef && trim == "" {
			inRef = false
		}
	}
	return paths
}

// noopEmbedder is used when gateway embedding is unavailable.
type noopEmbedder struct{}

func (noopEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("embedding unavailable")
}

var _ Embedder = (*GatewayEmbedder)(nil)
