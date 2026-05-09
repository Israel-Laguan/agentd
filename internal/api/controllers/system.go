package controllers

import (
	"net/http"

	"agentd/internal/api/httpx"
	"agentd/internal/services"
)

// SystemHandler exposes runtime telemetry derived from the deterministic
// status summarizer plus optional breaker/memory probes.
type SystemHandler struct {
	System *services.SystemService
}

// Get handles GET /api/v1/system/status.
func (h SystemHandler) Get(w http.ResponseWriter, r *http.Request) {
	if h.System == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "system service is not configured")
		return
	}
	snapshot, err := h.System.Snapshot(r.Context())
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	httpx.WriteSuccess(w, http.StatusOK, snapshot, nil)
}
