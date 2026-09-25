package controllers

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
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

type workspaceReadyResponse struct {
	Tasks []models.Task `json:"tasks"`
}

// List handles GET /api/v1/projects.
// Query params:
//   - include_system: include _system project in listing (default false)
func (h ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	projects, err := h.Store.ListProjects(r.Context())
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	if r.URL.Query().Get("include_system") != "true" {
		filtered := projects[:0]
		for _, p := range projects {
			if p.Name != "_system" {
				filtered = append(filtered, p)
			}
		}
		projects = filtered
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
	if err := verifyMaterializeToken(r, h.MaterializeToken); err != nil {
		httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden, err.Error())
		return
	}
	var plan models.DraftPlan
	if err := json.NewDecoder(r.Body).Decode(&plan); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid JSON request body")
		return
	}
	if h.Service == nil {
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "project service not configured")
		return
	}
	project, tasks, err := h.Service.MaterializePlan(r.Context(), plan)
	if err != nil {
		slog.Error("materialize endpoint: plan materialization failed", "project_name", plan.ProjectName, "error", err)
		httpx.WriteMappedError(w, err)
		return
	}
	slog.Info("materialize endpoint: plan materialized successfully", "project_id", project.ID, "task_count", len(tasks), "project_name", plan.ProjectName)
	httpx.WriteSuccess(w, http.StatusCreated, materializeResponse{Project: project, Tasks: tasks}, nil)
}

// WorkspaceReady signals that the project workspace has been populated and
// tasks may be dispatched. Transitions PENDING tasks to READY.
func (h ProjectHandler) WorkspaceReady(w http.ResponseWriter, r *http.Request) {
	if err := verifyMaterializeToken(r, h.MaterializeToken); err != nil {
		httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden, err.Error())
		return
	}
	projectID := r.PathValue("id")
	if h.Service == nil {
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "project service not configured")
		return
	}
	tasks, err := h.Service.MarkWorkspaceReady(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, models.ErrWorkspaceNotReady) {
			slog.Debug("workspace ready endpoint: workspace not yet populated", "project_id", projectID)
			httpx.WriteError(w, http.StatusConflict, httpx.CodeStateConflict, err.Error())
			return
		}
		slog.Error("workspace ready endpoint: failed to mark workspace ready", "project_id", projectID, "error", err)
		httpx.WriteMappedError(w, err)
		return
	}
	slog.Info("workspace ready endpoint: workspace marked ready and tasks unlocked", "project_id", projectID, "unlocked_count", len(tasks))
	httpx.WriteSuccess(w, http.StatusOK, workspaceReadyResponse{Tasks: tasks}, nil)
}

const materializeTokenHeader = "X-Agentd-Materialize-Token"

func verifyMaterializeToken(r *http.Request, wantValue string) error {
	want := strings.TrimSpace(wantValue)
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
