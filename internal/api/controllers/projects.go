package controllers

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"agentd/internal/api/httpx"
	"agentd/internal/models"
	"agentd/internal/services"
)

type ProjectHandler struct {
	Store            models.KanbanStore
	Service          *services.ProjectService
	MaterializeToken string
}

type materializeResponse struct {
	Project *models.Project `json:"project"`
	Tasks   []models.Task   `json:"tasks"`
}

func (h ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	projects, err := h.Store.ListProjects(r.Context())
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	httpx.WriteSuccess(w, http.StatusOK, projects, &httpx.Meta{Page: 1, PerPage: len(projects), Total: len(projects)})
}

func (h ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	project, err := h.Store.GetProject(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	httpx.WriteSuccess(w, http.StatusOK, project, nil)
}

func (h ProjectHandler) Materialize(w http.ResponseWriter, r *http.Request) {
	if err := h.verifyMaterializeToken(r); err != nil {
		httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden, err.Error())
		return
	}
	var plan models.DraftPlan
	if err := json.NewDecoder(r.Body).Decode(&plan); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid JSON request body")
		return
	}
	project, tasks, err := h.materialize(r, plan)
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	httpx.WriteSuccess(w, http.StatusCreated, materializeResponse{Project: project, Tasks: tasks}, nil)
}

func (h ProjectHandler) materialize(r *http.Request, plan models.DraftPlan) (*models.Project, []models.Task, error) {
	if h.Service != nil {
		return h.Service.MaterializePlan(r.Context(), plan)
	}
	return h.Store.MaterializePlan(r.Context(), plan)
}

const materializeTokenHeader = "X-Agentd-Materialize-Token"

func (h ProjectHandler) verifyMaterializeToken(r *http.Request) error {
	want := strings.TrimSpace(h.MaterializeToken)
	if want == "" {
		return nil
	}
	got := strings.TrimSpace(r.Header.Get(materializeTokenHeader))
	if got == "" {
		return errors.New("missing " + materializeTokenHeader + " header (configure api.materialize_token to require approval-aligned materialization)")
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return errors.New("invalid materialize token")
	}
	return nil
}
