package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"agentd/internal/config"
	"agentd/internal/models"
)

func (r *recallScenario) addGlobalMemory(_ context.Context, symptom string) error {
	r.memories = append(r.memories, models.Memory{
		ID: fmt.Sprintf("g-%d", len(r.memories)), Scope: "GLOBAL",
		Symptom:  sql.NullString{String: symptom, Valid: true},
		Solution: sql.NullString{String: "fix", Valid: true},
	})
	return nil
}

func (r *recallScenario) addProjectMemory(_ context.Context, projectID, symptom string) error {
	r.memories = append(r.memories, models.Memory{
		ID: fmt.Sprintf("p-%d", len(r.memories)), Scope: "TASK_CURATION",
		ProjectID: sql.NullString{String: projectID, Valid: true},
		Symptom:   sql.NullString{String: symptom, Valid: true},
		Solution:  sql.NullString{String: "fix", Valid: true},
	})
	return nil
}

func (r *recallScenario) addPreference(_ context.Context, userID, text string) error {
	r.memories = append(r.memories, models.Memory{
		ID: fmt.Sprintf("pref-%d", len(r.memories)), Scope: "USER_PREFERENCE",
		Tags:     sql.NullString{String: "user_id:" + userID, Valid: true},
		Symptom:  sql.NullString{String: "preference", Valid: true},
		Solution: sql.NullString{String: text, Valid: true},
	})
	return nil
}

func (r *recallScenario) addStoredPreference(_ context.Context, userID, text string) error {
	return r.addPreference(context.Background(), userID, text)
}

type fakeRecallStore struct {
	models.KanbanStore
	memories []models.Memory
}

func (s *fakeRecallStore) RecallMemories(_ context.Context, q models.RecallQuery) ([]models.Memory, error) {
	var out []models.Memory
	for _, m := range s.memories {
		if m.SupersededBy.Valid {
			continue
		}
		if includeRecallMemory(m, q) {
			out = append(out, m)
		}
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func includeRecallMemory(m models.Memory, q models.RecallQuery) bool {
	isGlobalScope := m.Scope == "GLOBAL"
	isCurrentProject := q.ProjectID != "" && m.ProjectID.Valid && m.ProjectID.String == q.ProjectID
	isPref := m.Scope == "USER_PREFERENCE" && q.UserID != "" && strings.Contains(m.Tags.String, "user_id:"+q.UserID)
	isOtherProject := m.ProjectID.Valid && q.ProjectID != "" && m.ProjectID.String != q.ProjectID

	if isOtherProject && !isGlobalScope && !isPref {
		return false
	}
	return isGlobalScope || isCurrentProject || isPref
}

func (s *fakeRecallStore) TouchMemories(context.Context, []string) error { return nil }

func (r *recallScenario) recallForProject(_ context.Context, projectID string) error {
	store := &fakeRecallStore{memories: r.memories}
	ret := &Retriever{Store: store, Cfg: config.LibrarianConfig{RecallTimeout: 5 * time.Second, RecallTopK: 20}}
	r.recalled = ret.Recall(context.Background(), "intent", projectID, "")
	return nil
}

func (r *recallScenario) recallForUser(_ context.Context, userID string) error {
	store := &fakeRecallStore{memories: r.memories}
	ret := &Retriever{Store: store, Cfg: config.LibrarianConfig{RecallTimeout: 5 * time.Second, RecallTopK: 20}}
	r.recalled = ret.Recall(context.Background(), "intent", "", userID)
	return nil
}

func (r *recallScenario) recallIncludes(_ context.Context, symptomOrSolution string) error {
	for _, m := range r.recalled {
		if m.Symptom.String == symptomOrSolution || m.Solution.String == symptomOrSolution {
			return nil
		}
	}
	return fmt.Errorf("recall should include %q but did not", symptomOrSolution)
}

func (r *recallScenario) recallExcludes(_ context.Context, symptomOrSolution string) error {
	for _, m := range r.recalled {
		if m.Symptom.String == symptomOrSolution || m.Solution.String == symptomOrSolution {
			return fmt.Errorf("recall should NOT include %q but did", symptomOrSolution)
		}
	}
	return nil
}

type hangStore struct {
	models.KanbanStore
}

func (s *hangStore) RecallMemories(ctx context.Context, _ models.RecallQuery) ([]models.Memory, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Second):
		return []models.Memory{{ID: "never"}}, nil
	}
}

func (s *hangStore) TouchMemories(context.Context, []string) error { return nil }

func (r *recallScenario) setHanging(context.Context) error {
	r.hanging = true
	return nil
}

func (r *recallScenario) recallWithTimeout(context.Context) error {
	ret := &Retriever{Store: &hangStore{}, Cfg: config.LibrarianConfig{RecallTimeout: 50 * time.Millisecond, RecallTopK: 5}}
	r.start = time.Now()
	r.recalled = ret.Recall(context.Background(), "intent", "", "")
	r.elapsed = time.Since(r.start)
	return nil
}

func (r *recallScenario) recallEmpty(context.Context) error {
	if len(r.recalled) != 0 {
		return fmt.Errorf("expected empty recall, got %d", len(r.recalled))
	}
	return nil
}

func (r *recallScenario) recallFast(context.Context) error {
	if r.elapsed > time.Second {
		return fmt.Errorf("recall took %s, should be < 1s", r.elapsed)
	}
	return nil
}
