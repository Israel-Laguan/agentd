package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// DreamAgent consolidates redundant memories during off-peak hours.
// It clusters similar memories, merges them via the LLM, and marks
// originals as superseded (Flow 5.3 / Danger C).
type DreamAgent struct {
	Store   models.KanbanStore
	Gateway gateway.AIGateway
	Breaker gateway.BreakerChecker
	Cfg     config.LibrarianConfig
}

// Run executes one dreaming cycle: cluster, merge, and prune.
func (d *DreamAgent) Run(ctx context.Context) error {
	if d.Store == nil {
		return nil
	}
	if d.Breaker != nil && d.Breaker.IsOpen() {
		slog.Info("dream agent skipped: circuit breaker open")
		return nil
	}

	memories, err := d.Store.ListUnsupersededMemories(ctx)
	if err != nil {
		return fmt.Errorf("dream: list memories: %w", err)
	}
	if len(memories) == 0 {
		return nil
	}

	clusters := d.cluster(memories)
	for _, cluster := range clusters {
		if err := d.mergeCluster(ctx, cluster); err != nil {
			slog.Error("dream merge failed", "cluster_size", len(cluster), "error", err)
		}
	}
	return nil
}

// cluster groups memories by overlapping symptom/solution keywords.
// Returns only groups meeting the minimum cluster size.
func (d *DreamAgent) cluster(memories []models.Memory) [][]models.Memory {
	minSize := d.Cfg.DreamClusterMinSize
	if minSize < 2 {
		minSize = 3
	}

	type entry struct {
		memory models.Memory
		tokens map[string]struct{}
	}
	entries := make([]entry, len(memories))
	for i, m := range memories {
		entries[i] = entry{memory: m, tokens: tokenize(m)}
	}

	used := make([]bool, len(entries))
	var clusters [][]models.Memory

	for i := range entries {
		if used[i] {
			continue
		}
		group := []models.Memory{entries[i].memory}
		used[i] = true
		for j := i + 1; j < len(entries); j++ {
			if used[j] {
				continue
			}
			if jaccard(entries[i].tokens, entries[j].tokens) >= d.similarityThreshold() {
				group = append(group, entries[j].memory)
				used[j] = true
			}
		}
		if len(group) >= minSize {
			clusters = append(clusters, group)
		}
	}
	return clusters
}

func (d *DreamAgent) similarityThreshold() float64 {
	if d.Cfg.DreamSimilarityThreshold > 0 {
		return d.Cfg.DreamSimilarityThreshold
	}
	return 0.3
}

func (d *DreamAgent) mergeCluster(ctx context.Context, cluster []models.Memory) error {
	if d.Gateway == nil {
		return fmt.Errorf("no AI gateway for dream merge")
	}
	merged, err := d.generateMergedSummary(ctx, cluster)
	if err != nil {
		return err
	}

	scope := cluster[0].Scope
	projectID := cluster[0].ProjectID

	newMem := models.Memory{
		Scope:     scope,
		ProjectID: projectID,
		Symptom:   sqlStr(merged.Symptom),
		Solution:  sqlStr(merged.Solution),
		Tags:      sqlStr("dream_consolidated"),
	}
	if err := d.Store.RecordMemory(ctx, newMem); err != nil {
		return fmt.Errorf("dream record merged memory: %w", err)
	}
	newID, err := d.findMergedMemoryID(ctx, merged)
	if err != nil {
		return err
	}
	if err := d.supersedeCluster(ctx, cluster, newID); err != nil {
		return fmt.Errorf("dream supersede old memories: %w", err)
	}

	slog.Info("dream agent merged cluster",
		"old_count", len(cluster),
		"new_id", newID,
		"symptom", merged.Symptom,
	)
	return nil
}

func (d *DreamAgent) generateMergedSummary(ctx context.Context, cluster []models.Memory) (memorySummary, error) {
	ctx = gateway.WithHouseRules(ctx, models.LoadHouseRules(ctx, d.Store))
	resp, err := d.Gateway.Generate(ctx, gateway.AIRequest{
		Messages: []gateway.PromptMessage{
			{Role: "system", Content: `You are given multiple similar lessons learned from past tasks. Synthesize them into one consolidated rule. If the lessons contradict each other, keep the most recent/accurate one. Output only valid JSON: {"symptom":"...","solution":"..."}.`},
			{Role: "user", Content: mergeClusterInput(cluster)},
		},
		Temperature:    0.1,
		JSONMode:       true,
		SkipTruncation: true,
	})
	if err != nil {
		return memorySummary{}, fmt.Errorf("dream LLM merge: %w", err)
	}
	var merged memorySummary
	if err := json.Unmarshal([]byte(resp.Content), &merged); err != nil {
		return memorySummary{}, fmt.Errorf("dream parse merged JSON: %w", err)
	}
	return merged, nil
}

func mergeClusterInput(cluster []models.Memory) string {
	parts := make([]string, 0, len(cluster))
	for _, m := range cluster {
		parts = append(parts, fmt.Sprintf("- Symptom: %s\n  Solution: %s", m.Symptom.String, m.Solution.String))
	}
	return strings.Join(parts, "\n")
}

func (d *DreamAgent) findMergedMemoryID(ctx context.Context, merged memorySummary) (string, error) {
	mems, err := d.Store.ListUnsupersededMemories(ctx)
	if err != nil {
		return "", fmt.Errorf("dream find new memory: %w", err)
	}
	for _, m := range mems {
		if m.Tags.Valid && m.Tags.String == "dream_consolidated" &&
			m.Symptom.String == merged.Symptom && m.Solution.String == merged.Solution {
			return m.ID, nil
		}
	}
	return "", fmt.Errorf("dream: could not locate newly recorded memory")
}

func (d *DreamAgent) supersedeCluster(ctx context.Context, cluster []models.Memory, newID string) error {
	oldIDs := make([]string, len(cluster))
	for i, m := range cluster {
		oldIDs[i] = m.ID
	}
	return d.Store.SupersedeMemories(ctx, oldIDs, newID)
}

func sqlStr(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func tokenize(m models.Memory) map[string]struct{} {
	text := strings.ToLower(m.Symptom.String + " " + m.Solution.String + " " + m.Tags.String)
	tokens := make(map[string]struct{})
	for _, w := range strings.Fields(text) {
		clean := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				return r
			}
			return -1
		}, w)
		if len(clean) >= 2 {
			tokens[clean] = struct{}{}
		}
	}
	return tokens
}

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	intersection := 0
	for k := range a {
		if _, ok := b[k]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
