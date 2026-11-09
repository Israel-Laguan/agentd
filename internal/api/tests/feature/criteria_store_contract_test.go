package api_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestCriteriaStoresEnforceUpdateContract(t *testing.T) {
	for name, makeStore := range map[string]func() (*models.Task, func(time.Time) (*models.Task, error)){
		"api": func() (*models.Task, func(time.Time) (*models.Task, error)) {
			s := newAPITestStore()
			return &s.task, func(expected time.Time) (*models.Task, error) {
				return s.UpdateTaskCriteria(context.Background(), s.task.ID, expected, []string{"new"}, nil)
			}
		},
		"http": func() (*models.Task, func(time.Time) (*models.Task, error)) {
			s := newTestStore()
			return &s.task, func(expected time.Time) (*models.Task, error) {
				return s.UpdateTaskCriteria(context.Background(), s.task.ID, expected, []string{"new"}, nil)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			task, update := makeStore()
			task.UpdatedAt = time.Now().UTC().Add(-time.Minute)
			task.CriteriaMet = []string{"old"}
			if _, err := update(task.UpdatedAt.Add(-time.Second)); !errors.Is(err, models.ErrStateConflict) {
				t.Fatalf("stale update: %v", err)
			}
			task.State = models.TaskStateRunning
			if _, err := update(task.UpdatedAt); !errors.Is(err, models.ErrStateConflict) {
				t.Fatalf("running update: %v", err)
			}
			task.State = models.TaskStateReady
			old := task.UpdatedAt
			got, err := update(old)
			if err != nil || len(got.CriteriaMet) != 0 || !got.UpdatedAt.After(old) {
				t.Fatalf("updated task = %+v, error = %v", got, err)
			}
		})
	}
}
