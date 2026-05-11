package config

import (
	"testing"
	"time"
)

func TestLibrarianConfig_Defaults(t *testing.T) {
	cfg := LibrarianConfig{
		RetentionHours:           defaultRetentionHours,
		ArchiveGraceDays:         defaultArchiveGraceDays,
		ChunkChars:               defaultChunkChars,
		MaxReducePasses:          defaultMaxReducePasses,
		FallbackHeadTailChars:    defaultFallbackHeadTailChars,
		RecallTimeout:            defaultRecallTimeout,
		RecallTopK:               defaultRecallTopK,
		PreferencesTopK:          defaultPreferencesTopK,
		DreamClusterMinSize:      defaultDreamClusterMinSize,
		DreamSimilarityThreshold: defaultDreamSimilarityThreshold,
	}
	if cfg.RetentionHours != 24 {
		t.Errorf("RetentionHours = %v, want 24", cfg.RetentionHours)
	}
	if cfg.ArchiveGraceDays != 7 {
		t.Errorf("ArchiveGraceDays = %v, want 7", cfg.ArchiveGraceDays)
	}
	if cfg.ChunkChars != 8000 {
		t.Errorf("ChunkChars = %v, want 8000", cfg.ChunkChars)
	}
	if cfg.MaxReducePasses != 3 {
		t.Errorf("MaxReducePasses = %v, want 3", cfg.MaxReducePasses)
	}
	if cfg.FallbackHeadTailChars != 2000 {
		t.Errorf("FallbackHeadTailChars = %v, want 2000", cfg.FallbackHeadTailChars)
	}
	if cfg.RecallTimeout != 500*time.Millisecond {
		t.Errorf("RecallTimeout = %v, want 500ms", cfg.RecallTimeout)
	}
	if cfg.RecallTopK != 5 {
		t.Errorf("RecallTopK = %v, want 5", cfg.RecallTopK)
	}
	if cfg.PreferencesTopK != 3 {
		t.Errorf("PreferencesTopK = %v, want 3", cfg.PreferencesTopK)
	}
	if cfg.DreamClusterMinSize != 3 {
		t.Errorf("DreamClusterMinSize = %v, want 3", cfg.DreamClusterMinSize)
	}
	if cfg.DreamSimilarityThreshold != 0.7 {
		t.Errorf("DreamSimilarityThreshold = %v, want 0.7", cfg.DreamSimilarityThreshold)
	}
}

func TestLibrarianConfig_Custom(t *testing.T) {
	cfg := LibrarianConfig{
		RetentionHours:           48,
		ArchiveGraceDays:         14,
		ChunkChars:               4000,
		MaxReducePasses:          5,
		FallbackHeadTailChars:    1000,
		RecallTimeout:             1 * time.Second,
		RecallTopK:                10,
		PreferencesTopK:          5,
		DreamClusterMinSize:      5,
		DreamSimilarityThreshold: 0.8,
	}
	if cfg.RetentionHours != 48 {
		t.Errorf("RetentionHours = %v, want 48", cfg.RetentionHours)
	}
	if cfg.DreamSimilarityThreshold != 0.8 {
		t.Errorf("DreamSimilarityThreshold = %v, want 0.8", cfg.DreamSimilarityThreshold)
	}
}

func TestDefaultLibrarianValues(t *testing.T) {
	if defaultRetentionHours != 24 {
		t.Errorf("defaultRetentionHours = %v", defaultRetentionHours)
	}
	if defaultArchiveGraceDays != 7 {
		t.Errorf("defaultArchiveGraceDays = %v", defaultArchiveGraceDays)
	}
	if defaultChunkChars != 8000 {
		t.Errorf("defaultChunkChars = %v", defaultChunkChars)
	}
	if defaultMaxReducePasses != 3 {
		t.Errorf("defaultMaxReducePasses = %v", defaultMaxReducePasses)
	}
	if defaultFallbackHeadTailChars != 2000 {
		t.Errorf("defaultFallbackHeadTailChars = %v", defaultFallbackHeadTailChars)
	}
	if DefaultRecallTimeout != 500*time.Millisecond {
		t.Errorf("DefaultRecallTimeout = %v", DefaultRecallTimeout)
	}
}