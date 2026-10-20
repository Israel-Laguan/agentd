package services

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
)

type fakeUsageStore struct {
	got models.AIUsageQuery
	rep models.AIUsageReport
	err error
}

func (f *fakeUsageStore) AIUsageReport(_ context.Context, q models.AIUsageQuery) (models.AIUsageReport, error) {
	f.got = q
	return f.rep, f.err
}

type fakeDrops uint64

func (d fakeDrops) Dropped() uint64 { return uint64(d) }

func TestAIUsageServiceDefaultsGroupByAndAddsDropped(t *testing.T) {
	t.Parallel()
	store := &fakeUsageStore{rep: models.AIUsageReport{Rows: []models.AIUsageRow{{Calls: 1}}}}
	svc := NewAIUsageService(store, fakeDrops(7))
	rep, err := svc.Report(context.Background(), models.AIUsageQuery{TaskID: "t1"})
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(store.got.GroupBy) != 1 || store.got.GroupBy[0] != models.AIDimPurpose || store.got.TaskID != "t1" {
		t.Fatalf("store query = %+v, want purpose default and the task filter", store.got)
	}
	if rep.Dropped != 7 || len(rep.Rows) != 1 {
		t.Fatalf("report = %+v, want 7 dropped and one row", rep)
	}
}

func TestAIUsageServiceRejectsABadQueryBeforeTheStore(t *testing.T) {
	t.Parallel()
	store := &fakeUsageStore{}
	_, err := NewAIUsageService(store, nil).Report(context.Background(), models.AIUsageQuery{GroupBy: []string{"colour"}})
	if !errors.Is(err, models.ErrInvalidAIUsageQuery) {
		t.Fatalf("err = %v, want ErrInvalidAIUsageQuery", err)
	}
	if store.got.GroupBy != nil {
		t.Fatal("the store was queried for an invalid request")
	}
}

func TestAIUsageServiceWithoutAStoreIsUnavailable(t *testing.T) {
	t.Parallel()
	if _, err := NewAIUsageService(nil, nil).Report(context.Background(), models.AIUsageQuery{}); !errors.Is(err, ErrAIUsageUnavailable) {
		t.Fatalf("err = %v, want ErrAIUsageUnavailable", err)
	}
}
