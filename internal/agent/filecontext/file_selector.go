package filecontext

import (
	"context"
	"log/slog"
	"math"
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
	return s.topScored(scored, pinned), nil
}

func (s *FileSelector) embedVectors(ctx context.Context, taskQuery string, docs []*CachedDoc) (queryVec []float32, docVecs [][]float32) {
	if s.embedder == nil || strings.TrimSpace(taskQuery) == "" {
		return nil, nil
	}

	docVecs = make([][]float32, len(docs))
	needEmbed := make([]int, 0, len(docs))
	for i, d := range docs {
		if len(d.Embedding) > 0 {
			docVecs[i] = d.Embedding
		} else {
			needEmbed = append(needEmbed, i)
		}
	}

	texts := make([]string, 0, len(needEmbed)+1)
	texts = append(texts, taskQuery)
	for _, i := range needEmbed {
		texts = append(texts, firstNTokens(docs[i].Markdown, embedSnippetTokens))
	}

	vecs, embedErr := s.embedder.Embed(ctx, texts)
	if embedErr != nil {
		slog.Warn("file selector embedding failed, using path order", "error", embedErr)
		return nil, nil
	}
	if len(vecs) != len(texts) {
		return nil, nil
	}
	for j, i := range needEmbed {
		docVecs[i] = vecs[j+1]
	}
	return vecs[0], docVecs
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

func (s *FileSelector) topScored(scored []scoredDoc, pinned map[string]struct{}) []*CachedDoc {
	out := make([]*CachedDoc, 0, len(scored))
	seen := make(map[string]struct{})

	for _, item := range scored {
		if pinned == nil {
			continue
		}
		key := normalizePathKey(item.doc.Path)
		if _, ok := pinned[key]; !ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item.doc)
	}

	for _, item := range scored {
		if len(out) >= s.topK {
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
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := 0; i < len(a); i++ {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func normalizePathKey(p string) string {
	return filepath.ToSlash(filepath.Clean(p))
}
