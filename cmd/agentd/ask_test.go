package main

import (
	"errors"
	"io"
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

func TestDecodeDraft_EmptyChoices(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"choices":[]}`)),
	}
	_, err := decodeDraft(resp)
	if err == nil {
		t.Fatal("decodeDraft() error = nil, want missing choices error")
	}
	if !strings.Contains(err.Error(), "missing choices") {
		t.Fatalf("error = %v, want missing choices message", err)
	}
	if strings.Contains(err.Error(), "status OK") {
		t.Fatalf("error = %v, should not report misleading status OK", err)
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
