package worker

import (
	"math"
	"strings"
	"unicode"
)

// ---------------------------------------------------------------------------
// TF-IDF implementation (lightweight, no external dependencies)
// ---------------------------------------------------------------------------

// tfidfIndex holds precomputed term frequencies and inverse document
// frequencies for a small corpus.
type tfidfIndex struct {
	docs []map[string]int // per-doc term counts
	idf  map[string]float64
}

// newTFIDF builds a TF-IDF index from a list of raw text documents.
func newTFIDF(docs []string) *tfidfIndex {
	idx := &tfidfIndex{
		docs: make([]map[string]int, len(docs)),
		idf:  make(map[string]float64),
	}

	// Document frequency per term.
	df := make(map[string]int)
	for i, doc := range docs {
		tf := tokenize(doc)
		idx.docs[i] = tf
		for term := range tf {
			df[term]++
		}
	}

	// Compute IDF: log(N / df(t)) + 1  (smoothed).
	n := float64(len(docs))
	for term, count := range df {
		idx.idf[term] = math.Log(n/float64(count)) + 1.0
	}
	return idx
}

// vector returns a sparse TF-IDF vector for the document at index i.
func (idx *tfidfIndex) vector(i int) map[string]float64 {
	vec := make(map[string]float64, len(idx.docs[i]))
	total := 0
	for _, count := range idx.docs[i] {
		total += count
	}
	if total == 0 {
		return vec
	}
	for term, count := range idx.docs[i] {
		tf := float64(count) / float64(total)
		vec[term] = tf * idx.idf[term]
	}
	return vec
}

// tokenize splits text into lowercased word tokens and returns a term
// frequency map.
func tokenize(text string) map[string]int {
	tf := make(map[string]int)
	lower := strings.ToLower(text)
	for _, word := range strings.FieldsFunc(lower, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_')
	}) {
		if len(word) > 1 {
			tf[word]++
		}
	}
	return tf
}

// cosineSimilarity computes the cosine similarity between two sparse vectors.
func cosineSimilarity(a, b map[string]float64) float64 {
	var dot, normA, normB float64
	for term, va := range a {
		normA += va * va
		if vb, ok := b[term]; ok {
			dot += va * vb
		}
	}
	for _, vb := range b {
		normB += vb * vb
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
