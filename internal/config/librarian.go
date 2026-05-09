package config

import (
	"time"

	"github.com/spf13/viper"
)

const (
	defaultRetentionHours            = 24
	defaultArchiveGraceDays          = 7
	defaultChunkChars                = 8000
	defaultMaxReducePasses           = 3
	defaultFallbackHeadTailChars     = 2000
	// DefaultRecallTimeout is the hard timeout for memory recall queries.
	DefaultRecallTimeout             = 500 * time.Millisecond
	defaultRecallTimeout             = DefaultRecallTimeout
	defaultRecallTopK                = 5
	defaultPreferencesTopK           = 3
	defaultDreamClusterMinSize       = 3
	defaultDreamSimilarityThreshold  = 0.7
)

// LibrarianConfig controls the two-phase log archival and memory curation job.
type LibrarianConfig struct {
	RetentionHours            int
	ArchiveGraceDays          int
	ChunkChars                int
	MaxReducePasses           int
	FallbackHeadTailChars     int
	RecallTimeout             time.Duration
	RecallTopK                int
	PreferencesTopK           int
	DreamClusterMinSize       int
	DreamSimilarityThreshold  float64
}

func setLibrarianDefaults(v *viper.Viper) {
	v.SetDefault("librarian.retention_hours", defaultRetentionHours)
	v.SetDefault("librarian.archive_grace_days", defaultArchiveGraceDays)
	v.SetDefault("librarian.chunk_chars", defaultChunkChars)
	v.SetDefault("librarian.max_reduce_passes", defaultMaxReducePasses)
	v.SetDefault("librarian.fallback_head_tail_chars", defaultFallbackHeadTailChars)
	v.SetDefault("librarian.recall_timeout", defaultRecallTimeout.String())
	v.SetDefault("librarian.recall_top_k", defaultRecallTopK)
	v.SetDefault("librarian.preferences_top_k", defaultPreferencesTopK)
	v.SetDefault("librarian.dream_cluster_min_size", defaultDreamClusterMinSize)
	v.SetDefault("librarian.dream_similarity_threshold", defaultDreamSimilarityThreshold)
}

func loadLibrarianConfig(v *viper.Viper) LibrarianConfig {
	recallTimeout, err := time.ParseDuration(v.GetString("librarian.recall_timeout"))
	if err != nil {
		recallTimeout = defaultRecallTimeout
	}
	return LibrarianConfig{
		RetentionHours:            v.GetInt("librarian.retention_hours"),
		ArchiveGraceDays:          v.GetInt("librarian.archive_grace_days"),
		ChunkChars:                v.GetInt("librarian.chunk_chars"),
		MaxReducePasses:           v.GetInt("librarian.max_reduce_passes"),
		FallbackHeadTailChars:     v.GetInt("librarian.fallback_head_tail_chars"),
		RecallTimeout:             recallTimeout,
		RecallTopK:                v.GetInt("librarian.recall_top_k"),
		PreferencesTopK:           v.GetInt("librarian.preferences_top_k"),
		DreamClusterMinSize:       v.GetInt("librarian.dream_cluster_min_size"),
		DreamSimilarityThreshold:  v.GetFloat64("librarian.dream_similarity_threshold"),
	}
}
