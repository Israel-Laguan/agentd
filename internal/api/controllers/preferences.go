package controllers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"agentd/internal/api/httpx"
	"agentd/internal/models"
)

// PreferencesHandler manages user preference storage.
type PreferencesHandler struct {
	Store models.KanbanStore
}

type preferenceRequest struct {
	UserID string `json:"user_id"`
	Text   string `json:"text"`
}

func (h PreferencesHandler) Save(w http.ResponseWriter, r *http.Request) {
	var req preferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid JSON request body")
		return
	}
	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.Text) == "" {
		httpx.WriteValidationError(w, http.StatusBadRequest, httpx.CodeValidation,
			"user_id and text are required", []string{"user_id must not be empty", "text must not be empty"})
		return
	}

	mem := models.Memory{
		Scope:    "USER_PREFERENCE",
		Tags:     sql.NullString{String: "user_id:" + req.UserID, Valid: true},
		Symptom:  sql.NullString{String: "preference", Valid: true},
		Solution: sql.NullString{String: req.Text, Valid: true},
	}
	if err := h.Store.RecordMemory(r.Context(), mem); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "failed to save preference")
		return
	}
	httpx.WriteSuccess(w, http.StatusCreated, map[string]string{"status": "saved"}, nil)
}
