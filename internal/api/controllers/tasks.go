package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"agentd/internal/api/httpx"
	"agentd/internal/models"
	"agentd/internal/services"
)

type TaskHandler struct {
	Store models.KanbanStore
	Tasks *services.TaskService
}

type commentRequest struct {
	Content string `json:"content"`
}

type patchRequest struct {
	State string `json:"state"`
}

type assignRequest struct {
	AgentID string `json:"agent_id"`
}

type splitRequest struct {
	Subtasks []splitSubtask `json:"subtasks"`
}

type splitSubtask struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// taskResponse augments models.Task with an embedded AgentProfile snapshot
// so the cockpit knows which agent is doing the work without an extra
// /api/v1/agents/{id} round-trip.
type taskResponse struct {
	models.Task
	Agent *agentResponse `json:"agent,omitempty"`
}

func (h TaskHandler) attachAgent(ctx context.Context, task *models.Task) taskResponse {
	resp := taskResponse{Task: *task}
	if h.Store == nil || strings.TrimSpace(task.AgentID) == "" {
		return resp
	}
	profile, err := h.Store.GetAgentProfile(ctx, task.AgentID)
	if err != nil || profile == nil {
		return resp
	}
	out := toAgentResponse(*profile)
	resp.Agent = &out
	return resp
}

func (h TaskHandler) attachAgents(ctx context.Context, tasks []models.Task) []taskResponse {
	if len(tasks) == 0 {
		return nil
	}
	cache := map[string]*agentResponse{}
	out := make([]taskResponse, len(tasks))
	for i, t := range tasks {
		out[i] = taskResponse{Task: t}
		id := strings.TrimSpace(t.AgentID)
		if id == "" || h.Store == nil {
			continue
		}
		if cached, ok := cache[id]; ok {
			out[i].Agent = cached
			continue
		}
		profile, err := h.Store.GetAgentProfile(ctx, id)
		if err != nil || profile == nil {
			cache[id] = nil
			continue
		}
		snapshot := toAgentResponse(*profile)
		cache[id] = &snapshot
		out[i].Agent = &snapshot
	}
	return out
}

// AddComment handles POST /api/v1/tasks/{id}/comments. The state guard
// (rejecting comments on terminal tasks) lives in the store transaction;
// this handler only enforces input shape and translates store sentinels.
func (h TaskHandler) AddComment(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	var req commentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid JSON request body")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		httpx.WriteValidationError(w, http.StatusBadRequest, httpx.CodeValidation,
			"comment content is required", []string{"content must not be empty"})
		return
	}
	comment, err := h.addComment(r, taskID, req.Content)
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	httpx.WriteSuccess(w, http.StatusCreated, comment, nil)
}

// Patch handles PATCH /api/v1/tasks/{id}. Today the only supported patch
// is a state transition, but the request shape leaves room for future
// editable fields.
func (h TaskHandler) Patch(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	var req patchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid JSON request body")
		return
	}
	if strings.TrimSpace(req.State) == "" {
		httpx.WriteValidationError(w, http.StatusBadRequest, httpx.CodeValidation,
			"state is required", []string{"state must be a known TaskState"})
		return
	}
	next := models.TaskState(strings.ToUpper(strings.TrimSpace(req.State)))
	if h.Tasks == nil {
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "task service is not configured")
		return
	}
	updated, err := h.Tasks.UpdateTaskState(r.Context(), taskID, next)
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	httpx.WriteSuccess(w, http.StatusOK, h.attachAgent(r.Context(), updated), nil)
}

// ListByProject handles GET /api/v1/projects/{id}/tasks with optional
// ?state=PENDING,RUNNING&assignee=HUMAN&limit=&offset= query parameters.
func (h TaskHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if h.Tasks == nil {
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "task service is not configured")
		return
	}
	filter, errs := parseTaskFilter(r)
	if len(errs) > 0 {
		httpx.WriteValidationError(w, http.StatusBadRequest, httpx.CodeValidation, "invalid query parameters", errs)
		return
	}
	page, err := h.Tasks.ListByProject(r.Context(), projectID, filter)
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	httpx.WriteSuccess(w, http.StatusOK, h.attachAgents(r.Context(), page.Data), httpx.MetaFromPagination(filter.Pagination, page.Total))
}

// Assign handles POST /api/v1/tasks/{id}/assign. The store rejects
// reassignment of a RUNNING task with an ErrStateConflict; the operator
// should pause via comments first if a live swap is intended.
func (h TaskHandler) Assign(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	var req assignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid JSON request body")
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		httpx.WriteValidationError(w, http.StatusBadRequest, httpx.CodeValidation,
			"agent_id is required", []string{"agent_id must not be empty"})
		return
	}
	if h.Tasks == nil {
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "task service is not configured")
		return
	}
	updated, err := h.Tasks.AssignAgent(r.Context(), taskID, req.AgentID)
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	httpx.WriteSuccess(w, http.StatusOK, h.attachAgent(r.Context(), updated), nil)
}

// Split handles POST /api/v1/tasks/{id}/split. Wraps the existing
// BlockTaskWithSubtasks primitive so a human can break down a stuck task.
func (h TaskHandler) Split(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	var req splitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid JSON request body")
		return
	}
	if len(req.Subtasks) == 0 {
		httpx.WriteValidationError(w, http.StatusBadRequest, httpx.CodeValidation,
			"subtasks is required", []string{"provide at least one subtask"})
		return
	}
	if h.Tasks == nil {
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "task service is not configured")
		return
	}
	drafts := make([]models.DraftTask, 0, len(req.Subtasks))
	for _, s := range req.Subtasks {
		drafts = append(drafts, models.DraftTask{
			Title: s.Title, Description: s.Description, Assignee: models.TaskAssigneeSystem,
		})
	}
	parent, children, err := h.Tasks.Split(r.Context(), taskID, drafts)
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	httpx.WriteSuccess(w, http.StatusCreated, map[string]any{
		"parent":   h.attachAgent(r.Context(), parent),
		"children": h.attachAgents(r.Context(), children),
	}, nil)
}

// Retry handles POST /api/v1/tasks/{id}/retry. Allowed from FAILED,
// FAILED_REQUIRES_HUMAN, or BLOCKED. Picks up the current agent_id and
// any prior comments left by the operator.
func (h TaskHandler) Retry(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	if h.Tasks == nil {
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "task service is not configured")
		return
	}
	updated, err := h.Tasks.Retry(r.Context(), taskID)
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	httpx.WriteSuccess(w, http.StatusOK, h.attachAgent(r.Context(), updated), nil)
}

func (h TaskHandler) addComment(r *http.Request, taskID, content string) (any, error) {
	if h.Tasks != nil {
		comment, err := h.Tasks.AddHumanComment(r.Context(), taskID, content)
		if err != nil {
			return nil, err
		}
		return comment, nil
	}
	comment := models.Comment{TaskID: taskID, Author: models.CommentAuthorUser, Body: content, Content: content}
	if board, ok := any(h.Store).(models.KanbanBoardContract); ok {
		if err := board.AddCommentAndPause(r.Context(), taskID, comment); err != nil {
			return nil, err
		}
		return map[string]string{"task_id": taskID}, nil
	}
	if err := h.Store.AddComment(r.Context(), comment); err != nil {
		return nil, err
	}
	return map[string]string{"task_id": taskID}, nil
}
