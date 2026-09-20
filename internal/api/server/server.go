package server

import (
	"net/http"

	"agentd/internal/api/controllers"
	"agentd/internal/api/sse"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/memory"
	"agentd/internal/models"
	"agentd/internal/services"
)

// ServerDeps bundles dependencies for the HTTP API.
type ServerDeps struct {
	Addr             string
	Store            models.KanbanStore
	Gateway          gateway.AIGateway
	Bus              bus.Bus
	Project          *services.ProjectService
	MaterializeToken string
	Tasks            *services.TaskService
	System           *services.SystemService
	Agents           *services.AgentService
	Summarizer       *frontdesk.StatusSummarizer
	FileStash        *frontdesk.FileStash
	Truncator        gateway.Truncator
	Budget           int
	Hub              *sse.Hub
	Retriever        *memory.Retriever
	ProviderConfigs  []spec.ProviderConfig
}

// NewHandler builds the API mux.
func NewHandler(deps ServerDeps) http.Handler {
	mux := http.NewServeMux()
	projects := controllers.ProjectHandler{
		Store: deps.Store, Service: deps.Project,
		MaterializeToken: deps.MaterializeToken,
	}
	tasks := controllers.TaskHandler{Store: deps.Store, Tasks: resolveTaskService(deps)}
	chat := controllers.ChatHandler{
		Planner: &frontdesk.Planner{
			Gateway: deps.Gateway, Summarizer: deps.Summarizer,
			SettingsStore: deps.Store,
			Stash:         deps.FileStash, Truncator: deps.Truncator, Budget: deps.Budget,
		},
		Retriever: deps.Retriever,
	}
	stream := sse.Handler{Bus: deps.Bus, Hub: deps.Hub}

	preferences := controllers.PreferencesHandler{Store: deps.Store}
	system := controllers.SystemHandler{System: resolveSystemService(deps)}
	agents := controllers.AgentHandler{Service: resolveAgentService(deps)}
	gway := controllers.GatewayHandler{Configs: toProviderEntries(deps.ProviderConfigs)}

	mux.HandleFunc("GET /api/v1/projects", projects.List)
	mux.HandleFunc("GET /api/v1/projects/{id}", projects.Get)
	mux.HandleFunc("GET /api/v1/projects/{id}/tasks", tasks.ListByProject)
	mux.HandleFunc("POST /api/v1/projects/materialize", projects.Materialize)
	mux.HandleFunc("POST /api/v1/projects/{id}/workspace/ready", projects.WorkspaceReady)
	mux.HandleFunc("GET /api/v1/tasks/{id}/comments", tasks.ListComments)
	mux.HandleFunc("POST /api/v1/tasks/{id}/comments", tasks.AddComment)
	mux.HandleFunc("PATCH /api/v1/tasks/{id}", tasks.Patch)
	mux.HandleFunc("POST /api/v1/tasks/{id}/assign", tasks.Assign)
	mux.HandleFunc("POST /api/v1/tasks/{id}/split", tasks.Split)
	mux.HandleFunc("POST /api/v1/tasks/{id}/retry", tasks.Retry)
	mux.HandleFunc("GET /api/v1/agents", agents.List)
	mux.HandleFunc("GET /api/v1/agents/{id}", agents.Get)
	mux.HandleFunc("POST /api/v1/agents", agents.Create)
	mux.HandleFunc("PATCH /api/v1/agents/{id}", agents.Patch)
	mux.HandleFunc("DELETE /api/v1/agents/{id}", agents.Delete)
	mux.HandleFunc("GET /api/v1/gateway/providers", gway.List)
	mux.HandleFunc("GET /api/v1/system/status", system.Get)
	mux.HandleFunc("POST /api/v1/system/breaker/reset", system.Reset)
	mux.HandleFunc("GET /api/v1/events/stream", stream.ServeHTTP)
	mux.HandleFunc("POST /v1/chat/completions", chat.Complete)
	mux.HandleFunc("POST /api/v1/preferences", preferences.Save)

	// B-001: discovery endpoints. The mux previously only served
	// /api/v1/*, so the daemon root and the conventional /health and /docs
	// paths all returned the mux 404. A headless daemon still needs a
	// landing page and a health check.
	mux.HandleFunc("GET /{$}", handleLanding)
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /docs", handleDocs)
	return corsMiddleware(mux)
}

// handleLanding serves a minimal daemon landing page so GET / identifies the
// service and points at the API instead of returning the mux 404.
func handleLanding(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"service":"agentd","status":"ok","health":"/health","docs":"/docs","api":"/api/v1"}` + "\n"))
}

// handleHealth is the lightweight liveness probe (no store access).
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
}

// handleDocs serves a minimal human-readable endpoint catalog. It is a
// static overview, not full OpenAPI/Swagger — enough to explore the daemon
// without grepping the source.
func handleDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>agentd API</title></head>
<body>
<h1>agentd API</h1>
<p>Health: <a href="/health">/health</a> &middot; JSON API under <code>/api/v1</code> &middot; OpenAI-compatible chat at <code>POST /v1/chat/completions</code></p>
<ul>
<li>GET /api/v1/projects, GET /api/v1/projects/{id}, GET /api/v1/projects/{id}/tasks</li>
<li>POST /api/v1/projects/materialize, POST /api/v1/projects/{id}/workspace/ready</li>
<li>GET/POST /api/v1/tasks/{id}/comments, PATCH /api/v1/tasks/{id}</li>
<li>POST /api/v1/tasks/{id}/assign, /split, /retry</li>
<li>GET /api/v1/agents, POST /api/v1/agents, GET/PATCH/DELETE /api/v1/agents/{id}, GET /api/v1/gateway/providers</li>
<li>GET /api/v1/system/status, POST /api/v1/system/breaker/reset</li>
<li>GET /api/v1/events/stream (SSE), POST /api/v1/preferences</li>
</ul>
</body>
</html>
`))
}

// corsAllowedOrigins is the set of origins permitted to make cross-origin
// requests to the daemon. Restricted to the local Next.js dev server so that
// arbitrary websites cannot read daemon API responses in a user's browser.
var corsAllowedOrigins = map[string]struct{}{
	"http://localhost:3000": {},
}

// corsMiddleware adds CORS headers for allowed origins so the Next.js dev
// server (localhost:3000) can reach the daemon (localhost:8765) without a
// proxy. Origins not in the allowlist receive no CORS headers.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if _, ok := corsAllowedOrigins[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}
		if r.Method == http.MethodOptions && origin != "" {
			if _, ok := corsAllowedOrigins[origin]; ok {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func resolveAgentService(deps ServerDeps) *services.AgentService {
	if deps.Agents != nil {
		return deps.Agents
	}
	if deps.Store == nil {
		return nil
	}
	var bridge services.AgentBus
	if deps.Bus != nil {
		bridge = bus.AgentBridge{Bus: deps.Bus}
	}
	svc := services.NewAgentService(deps.Store, bridge)
	if lister, ok := deps.Gateway.(services.ProviderLister); ok {
		svc.Lister = lister
	}
	return svc
}

func resolveTaskService(deps ServerDeps) *services.TaskService {
	svc := deps.Tasks
	if svc == nil {
		if deps.Store == nil {
			return nil
		}
		board, _ := any(deps.Store).(models.KanbanBoardContract)
		svc = services.NewTaskService(deps.Store, board)
	}
	if deps.Bus != nil && svc != nil {
		svc = svc.WithBus(bus.TaskBridge{Bus: deps.Bus})
	}
	return svc
}

func resolveSystemService(deps ServerDeps) *services.SystemService {
	if deps.System != nil {
		return deps.System
	}
	if deps.Summarizer == nil {
		return nil
	}
	return services.NewSystemService(deps.Summarizer, nil)
}

// toProviderEntries converts full ProviderConfig slices (which carry API keys
// and base URLs) into the minimal wire-safe summary used by GatewayHandler.
// Stripping credentials here ensures they never reside inside an HTTP handler.
func toProviderEntries(cfgs []spec.ProviderConfig) []controllers.ProviderEntry {
	entries := make([]controllers.ProviderEntry, 0, len(cfgs))
	for _, cfg := range cfgs {
		models := make([]string, 0, 1)
		if cfg.Model != "" {
			models = append(models, cfg.Model)
		}
		entries = append(entries, controllers.ProviderEntry{
			Name:    cfg.Name,
			Adapter: cfg.Adapter,
			Models:  models,
		})
	}
	return entries
}
