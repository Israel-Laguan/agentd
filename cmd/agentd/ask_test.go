package main

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"agentd/internal/config"
)

func TestMapDraftAPIError_ProviderMessages(t *testing.T) {
	body := []byte(`{"status":"error","error":{"message":"internal error: no LLM providers configured"}}`)
	err := mapDraftAPIError(http.StatusInternalServerError, body)
	if !errors.Is(err, config.ErrNoLLMProviders) {
		t.Fatalf("error = %v, want %v", err, config.ErrNoLLMProviders)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %v, want HTTP status in message", err)
	}
}

func TestMapDraftAPIError_Generic(t *testing.T) {
	err := mapDraftAPIError(http.StatusBadRequest, []byte(`{"error":"bad"}`))
	if errors.Is(err, config.ErrNoLLMProviders) {
		t.Fatalf("error = %v, want non-provider error", err)
	}
	if !strings.Contains(err.Error(), "Bad Request") {
		t.Fatalf("error = %v, want status text", err)
	}
}
