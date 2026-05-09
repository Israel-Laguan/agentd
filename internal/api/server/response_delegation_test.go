package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentd/internal/models"
)

func TestWriteDelegatesHitServerWrappers(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteSuccess(rec, http.StatusOK, map[string]string{"k": "v"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	WriteError(rec, http.StatusTeapot, CodeBadRequest, "msg")
	if rec.Code != http.StatusTeapot {
		t.Fatalf("code = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	WriteValidationError(rec, http.StatusBadRequest, CodeValidation, "bad", []string{"a", "b"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	WriteMappedError(rec, errors.New("x"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	WriteJSON(rec, http.StatusNoContent, map[string]int{})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code = %d", rec.Code)
	}
	m := MetaFromPagination(models.PaginationParams{Offset: 5, Limit: 5}, 12)
	if m == nil || m.Page != 2 {
		t.Fatalf("meta = %#v", m)
	}
	st, code, msg := MapError(models.ErrProjectNotFound)
	if st != http.StatusNotFound || code != CodeNotFound {
		t.Fatalf("MapError = %d %q %q", st, code, msg)
	}
}
