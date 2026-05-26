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

// NewServer returns an http.Server with the API handler.
func NewServer(deps ServerDeps) *http.Server {
	return &http.Server{Addr: deps.Addr, Handler: NewHandler(deps)}
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
	gway := controllers.GatewayHandler{Configs: deps.ProviderConfigs}

	mux.HandleFunc("GET /api/v1/projects", projects.List)
	mux.HandleFunc("GET /api/v1/projects/{id}", projects.Get)
	mux.HandleFunc("GET /api/v1/projects/{id}/tasks", tasks.ListByProject)
	mux.HandleFunc("POST /api/v1/projects/materialize", projects.Materialize)
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
	return corsMiddleware(mux)
}

// corsMiddleware adds permissive CORS headers so the Next.js dev server
// (localhost:3000) can reach the daemon (localhost:8765) without a proxy.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
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
	return services.NewAgentService(deps.Store, bridge)
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
