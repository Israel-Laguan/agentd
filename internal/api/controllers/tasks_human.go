package controllers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"agentd/internal/api/httpx"
	"agentd/internal/models"
)

type humanResolutionRequest struct {
	Result            string     `json:"result"`
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}

const maxHumanResolutionBody = 20 * 1024

func (h TaskHandler) ResolveHumanHandoff(w http.ResponseWriter, r *http.Request) {
	if err := verifyMaterializeToken(r, h.MaterializeToken); err != nil {
		httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden, err.Error())
		return
	}
	if h.HumanResolver == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "human handoff resolution is not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxHumanResolutionBody)
	var req humanResolutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid JSON request body")
		return
	}
	if strings.TrimSpace(req.Result) == "" {
		httpx.WriteValidationError(w, http.StatusBadRequest, httpx.CodeValidation,
			"human result is required", []string{"result must not be empty"})
		return
	}
	resolution, err := h.HumanResolver.ResolveHumanHandoff(
		r.Context(), r.PathValue("id"), req.ExpectedUpdatedAt, req.Result,
	)
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	httpx.WriteSuccess(w, http.StatusOK, resolution, nil)
}

var _ models.HumanHandoffResolver
