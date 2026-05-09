package controllers

import (
	"encoding/json"
	"net/http"
	"strings"

	"agentd/internal/api/httpx"
	"agentd/internal/models"
	"agentd/internal/services"

	"github.com/google/uuid"
)

// AgentHandler exposes CRUD endpoints over the AgentProfile registry. The
// store enforces deletion protections (default profile, in-use); this
// handler only translates HTTP shape and validation errors.
type AgentHandler struct {
	Service *services.AgentService
}

type agentResponse struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	Temperature  float64 `json:"temperature"`
	SystemPrompt string  `json:"system_prompt,omitempty"`
	Role         string  `json:"role"`
	MaxTokens    int     `json:"max_tokens"`
	UpdatedAt    string  `json:"updated_at"`
}

type agentCreateRequest struct {
	ID           string  `json:"id,omitempty"`
	Name         string  `json:"name"`
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	Temperature  float64 `json:"temperature,omitempty"`
	SystemPrompt string  `json:"system_prompt,omitempty"`
	Role         string  `json:"role,omitempty"`
	MaxTokens    int     `json:"max_tokens,omitempty"`
}

type agentPatchRequest struct {
	Name         *string  `json:"name,omitempty"`
	Provider     *string  `json:"provider,omitempty"`
	Model        *string  `json:"model,omitempty"`
	Temperature  *float64 `json:"temperature,omitempty"`
	SystemPrompt *string  `json:"system_prompt,omitempty"`
	Role         *string  `json:"role,omitempty"`
	MaxTokens    *int     `json:"max_tokens,omitempty"`
}

// List handles GET /api/v1/agents.
func (h AgentHandler) List(w http.ResponseWriter, r *http.Request) {
	profiles, err := h.Service.List(r.Context())
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	out := make([]agentResponse, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, toAgentResponse(p))
	}
	httpx.WriteSuccess(w, http.StatusOK, out, &httpx.Meta{Page: 1, PerPage: len(out), Total: len(out)})
}

// Get handles GET /api/v1/agents/{id}.
func (h AgentHandler) Get(w http.ResponseWriter, r *http.Request) {
	profile, err := h.Service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	httpx.WriteSuccess(w, http.StatusOK, toAgentResponse(*profile), nil)
}

// Create handles POST /api/v1/agents.
func (h AgentHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req agentCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid JSON request body")
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = uuid.NewString()
	}
	profile := models.AgentProfile{
		ID: id, Name: req.Name, Provider: req.Provider, Model: req.Model,
		Temperature: req.Temperature, Role: req.Role, MaxTokens: req.MaxTokens,
	}
	if req.SystemPrompt != "" {
		profile.SystemPrompt.Valid = true
		profile.SystemPrompt.String = req.SystemPrompt
	}
	created, err := h.Service.Create(r.Context(), profile)
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	httpx.WriteSuccess(w, http.StatusCreated, toAgentResponse(*created), nil)
}

// Patch handles PATCH /api/v1/agents/{id}.
func (h AgentHandler) Patch(w http.ResponseWriter, r *http.Request) {
	var req agentPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid JSON request body")
		return
	}
	patch := services.AgentPatch{
		Name: req.Name, Provider: req.Provider, Model: req.Model,
		Temperature: req.Temperature, SystemPrompt: req.SystemPrompt,
		Role: req.Role, MaxTokens: req.MaxTokens,
	}
	updated, err := h.Service.Patch(r.Context(), r.PathValue("id"), patch)
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	httpx.WriteSuccess(w, http.StatusOK, toAgentResponse(*updated), nil)
}

// Delete handles DELETE /api/v1/agents/{id}.
func (h AgentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.Delete(r.Context(), r.PathValue("id")); err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	httpx.WriteSuccess(w, http.StatusOK, map[string]string{"id": r.PathValue("id"), "status": "deleted"}, nil)
}

func toAgentResponse(p models.AgentProfile) agentResponse {
	out := agentResponse{
		ID: p.ID, Name: p.Name, Provider: p.Provider, Model: p.Model,
		Temperature: p.Temperature, Role: p.Role, MaxTokens: p.MaxTokens,
		UpdatedAt: p.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
	}
	if p.SystemPrompt.Valid {
		out.SystemPrompt = p.SystemPrompt.String
	}
	return out
}
