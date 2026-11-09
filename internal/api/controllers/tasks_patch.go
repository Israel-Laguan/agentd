package controllers

import (
	"net/http"
	"strings"

	"agentd/internal/api/httpx"
	"agentd/internal/models"
)

type patchRequest struct {
	State           string                    `json:"state"`
	Description     *string                   `json:"description,omitempty"`
	SuccessCriteria *[]string                 `json:"success_criteria,omitempty"`
	GoldenCriteria  *[]models.GoldenCriterion `json:"golden_criteria,omitempty"`
}

// Patch handles PATCH /api/v1/tasks/{id}.
func (h TaskHandler) Patch(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	var req patchRequest
	if err := httpx.DecodeJSON(w, r, httpx.DefaultMaxBody, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid JSON request body")
		return
	}
	hasState := strings.TrimSpace(req.State) != ""
	hasDescription := req.Description != nil
	hasCriteria := req.SuccessCriteria != nil || req.GoldenCriteria != nil
	if !hasState && !hasDescription && !hasCriteria {
		httpx.WriteValidationError(w, http.StatusBadRequest, httpx.CodeValidation,
			"at least one of state, description, success_criteria, or golden_criteria is required",
			[]string{"state, description, success_criteria, or golden_criteria must be provided"})
		return
	}
	if hasCriteria && (hasState || hasDescription) {
		httpx.WriteValidationError(w, http.StatusBadRequest, httpx.CodeValidation, "criteria must be updated separately", []string{"send criteria in a separate request"})
		return
	}
	if h.Tasks == nil {
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "task service is not configured")
		return
	}
	var updated *models.Task
	var err error
	if hasCriteria {
		updated, err = h.Tasks.UpdateTaskCriteria(r.Context(), taskID, req.SuccessCriteria, req.GoldenCriteria)
	} else if hasState && hasDescription {
		next := models.TaskState(strings.ToUpper(strings.TrimSpace(req.State)))
		updated, err = h.Tasks.PatchTask(r.Context(), taskID, &next, req.Description)
	} else if hasState {
		next := models.TaskState(strings.ToUpper(strings.TrimSpace(req.State)))
		updated, err = h.Tasks.UpdateTaskState(r.Context(), taskID, next)
	} else {
		updated, err = h.Tasks.UpdateTaskDescription(r.Context(), taskID, *req.Description)
	}
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	httpx.WriteSuccess(w, http.StatusOK, h.attachAgent(r.Context(), updated), nil)
}
