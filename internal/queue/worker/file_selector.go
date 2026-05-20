package worker

import (
	"context"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
)

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
	queryVec, docVecs := s.embedVectors(ctx, taskQuery, docs)
	scored := s.scoreDocs(docs, queryVec, docVecs, pinned)
	return s.topScored(scored), nil
}

func (s *FileSelector) embedVectors(ctx context.Context, taskQuery string, docs []*CachedDoc) (queryVec []float32, docVecs [][]float32) {
	if s.embedder == nil || strings.TrimSpace(taskQuery) == "" {
		return nil, nil
	}
	texts := make([]string, 0, len(docs)+1)
	texts = append(texts, taskQuery)
	for _, d := range docs {
		texts = append(texts, firstNTokens(d.Markdown, embedSnippetTokens))
	}
	vecs, embedErr := s.embedder.Embed(ctx, texts)
	if embedErr != nil {
		slog.Warn("file selector embedding failed, using path order", "error", embedErr)
		return nil, nil
	}
	if len(vecs) != len(texts) {
		return nil, nil
	}
	return vecs[0], vecs[1:]
}

func (s *FileSelector) scoreDocs(docs []*CachedDoc, queryVec []float32, docVecs [][]float32, pinned map[string]struct{}) []scoredDoc {
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
	return scored
}

func (s *FileSelector) topScored(scored []scoredDoc) []*CachedDoc {
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
	return out
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
