package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentd/internal/models"
)

func TestWriteSuccessAndMeta(t *testing.T) {
	rec := httptest.NewRecorder()
	meta := &Meta{Page: 2, PerPage: 10, Total: 25}
	WriteSuccess(rec, http.StatusOK, map[string]string{"id": "1"}, meta)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var env Envelope
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Status != "success" {
		t.Fatalf("status = %q", env.Status)
	}
}

func TestWriteMappedErrorAgentNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteMappedError(rec, fmt.Errorf("wrap: %w", models.ErrAgentProfileNotFound))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestWriteMappedErrorDefaultInternal(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteMappedError(rec, errors.New("mystery"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestMetaFromPagination(t *testing.T) {
	m := MetaFromPagination(models.PaginationParams{Offset: 20, Limit: 10}, 55)
	if m.Page != 3 || m.PerPage != 10 || m.Total != 55 {
		t.Fatalf("meta = %#v", m)
	}
	m = MetaFromPagination(models.PaginationParams{Limit: 0}, 7)
	if m.Page != 1 || m.PerPage != 7 || m.Total != 7 {
		t.Fatalf("zero limit meta = %#v", m)
	}
	m = MetaFromPagination(models.PaginationParams{Offset: -5, Limit: 5}, 1)
	if m.Page < 1 {
		t.Fatalf("page = %d", m.Page)
	}
}

func TestMapErrorSentinels(t *testing.T) {
	cases := []struct {
		err      error
		wantCode int
		wantID   string
	}{
		{models.ErrProjectNotFound, http.StatusNotFound, CodeNotFound},
		{models.ErrSandboxViolation, http.StatusForbidden, CodeForbidden},
		{models.ErrInvalidDraftPlan, http.StatusBadRequest, CodeValidation},
		{models.ErrStateConflict, http.StatusConflict, CodeStateConflict},
	}
	for _, tc := range cases {
		st, code, _ := MapError(tc.err)
		if st != tc.wantCode || code != tc.wantID {
			t.Fatalf("%v -> %d %q, want %d %q", tc.err, st, code, tc.wantCode, tc.wantID)
		}
	}
}
