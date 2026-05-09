// Package controllers wires HTTP handlers to service-layer logic. The chat
// handler exposes an OpenAI-compatible POST /v1/chat/completions endpoint
// that routes the last user message through Frontdesk before planning.
//
// Wire compatibility is asserted by round-tripping our emitted JSON through
// github.com/openai/openai-go/v3 response types in tests; see the related
// chat tests under internal/api.
package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"agentd/internal/api/httpx"
	"agentd/internal/frontdesk"
	"agentd/internal/gateway"
	"agentd/internal/memory"
	"agentd/internal/models"

	"github.com/google/uuid"
)

// ChatHandler implements the OpenAI-compatible chat completions endpoint.
type ChatHandler struct {
	Planner   *frontdesk.Planner
	Retriever *memory.Retriever
}

// chatRequest mirrors the OpenAI Chat Completions wire shape with two
// agentd extensions (approved_scopes, files). We decode into our own struct
// rather than openai.ChatCompletionNewParams because the SDK's request
// types use param.Opt[T] / OfX union fields with omitzero semantics and do
// not unmarshal cleanly from arbitrary JSON.
type chatRequest struct {
	Model       string                  `json:"model"`
	Messages    []gateway.PromptMessage `json:"messages"`
	MaxTokens   int                     `json:"max_tokens,omitempty"`
	Stream      bool                    `json:"stream,omitempty"`
	Temperature float64                 `json:"temperature,omitempty"`
	// Tools and ToolChoice are accepted for OpenAI wire compat but not
	// validated; presence enables tool_calls in the response (see
	// includeToolCalls below).
	Tools      []json.RawMessage `json:"tools,omitempty"`
	ToolChoice json.RawMessage   `json:"tool_choice,omitempty"`
	// agentd extensions
	ApprovedScopes []string   `json:"approved_scopes,omitempty"`
	Files          []chatFile `json:"files,omitempty"`
}

type chatFile struct {
	Name    string `json:"name,omitempty"`
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"`
}

// Complete handles POST /v1/chat/completions.
func (h ChatHandler) Complete(w http.ResponseWriter, r *http.Request) {
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "could not read request body")
		return
	}
	var req chatRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid JSON request body")
		return
	}
	rawMessage := frontdesk.LastUserMessage(req.Messages)
	if rawMessage == "" {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "a user message is required")
		return
	}

	intent, files, err := frontdesk.PrepareIntent(h.Planner.Stash, rawMessage, convertFiles(req.Files))
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}

	var projectID string
	if len(req.ApprovedScopes) == 1 {
		projectID = req.ApprovedScopes[0]
	}
	userID := r.Header.Get("X-Agentd-User")
	intent = h.prependRecalledContext(r.Context(), intent, projectID, userID)

	if req.Stream {
		h.completeStreaming(w, r, req, intent, files)
		return
	}

	content, err := h.Planner.PlanContent(r.Context(), req.ApprovedScopes, intent, files)
	if err != nil {
		if errors.Is(err, frontdesk.ErrMultipleApprovedScopes) {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "send one turn per scope using approved_scopes with exactly one entry")
			return
		}
		if isAICoreTimeout(err) {
			httpx.WriteJSON(w, http.StatusOK, completion(req.Model, systemTimeoutMessage, nil, finishReasonStop))
			return
		}
		httpx.WriteMappedError(w, err)
		return
	}
	toolCalls, finishReason := buildToolCalls(content, len(req.Tools) > 0)
	httpx.WriteJSON(w, http.StatusOK, completion(req.Model, string(content), toolCalls, finishReason))
}

// completeStreaming emits the response as an SSE stream of chatChunk
// frames terminated by "data: [DONE]\n\n", matching OpenAI's stream:true
// wire format.
func (h ChatHandler) completeStreaming(
	w http.ResponseWriter,
	r *http.Request,
	req chatRequest,
	intent string,
	files []frontdesk.FileRef,
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	id := "chatcmpl-" + uuid.NewString()
	model := req.Model
	if model == "" {
		model = "agentd"
	}

	writeFrame := func(delta chatChunkDelta, finish *string) {
		chunk := chatChunk{
			ID: id, Object: chatObjectChunk, Created: time.Now().Unix(), Model: model,
			Choices: []chatChunkChoice{{Index: 0, Delta: delta, FinishReason: finish}},
		}
		payload, err := json.Marshal(chunk)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	}

	writeFrame(chatChunkDelta{Role: "assistant"}, nil)

	content, err := h.Planner.PlanContent(r.Context(), req.ApprovedScopes, intent, files)
	switch {
	case errors.Is(err, frontdesk.ErrMultipleApprovedScopes):
		writeFrame(chatChunkDelta{Content: "send one turn per scope using approved_scopes with exactly one entry"}, ptr(finishReasonStop))
	case isAICoreTimeout(err):
		writeFrame(chatChunkDelta{Content: systemTimeoutMessage}, ptr(finishReasonStop))
	case err != nil:
		writeFrame(chatChunkDelta{Content: fmt.Sprintf("error: %v", err)}, ptr(finishReasonStop))
	default:
		toolCalls, finish := buildToolCalls(content, len(req.Tools) > 0)
		writeFrame(chatChunkDelta{Content: string(content), ToolCalls: toolCalls}, ptr(finish))
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (h ChatHandler) prependRecalledContext(ctx context.Context, intent, projectID, userID string) string {
	if h.Retriever == nil {
		return intent
	}
	recalled := h.Retriever.Recall(ctx, intent, projectID, userID)
	if len(recalled) == 0 {
		return intent
	}
	lessons := memory.FormatLessons(recalled)
	prefs := memory.FormatPreferences(recalled)
	var prefix string
	if prefs != "" {
		prefix += prefs + "\n\n"
	}
	if lessons != "" {
		prefix += lessons + "\n\n"
	}
	if prefix == "" {
		return intent
	}
	return prefix + intent
}

func convertFiles(files []chatFile) []frontdesk.InputFile {
	if len(files) == 0 {
		return nil
	}
	out := make([]frontdesk.InputFile, len(files))
	for i, f := range files {
		out[i] = frontdesk.InputFile{Name: f.Name, Path: f.Path, Content: f.Content}
	}
	return out
}

func isAICoreTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, models.ErrLLMUnreachable)
}

// buildToolCalls inspects the JSON content returned by Frontdesk and
// constructs an OpenAI-compatible tool_calls slice when the content
// represents a plan or a status report. Tools are only emitted when the
// client opted in by sending req.Tools (preserves wire-shape stability for
// existing simple clients that just consume content).
func buildToolCalls(content []byte, toolsRequested bool) ([]chatToolCall, string) {
	if !toolsRequested || len(content) == 0 {
		return nil, finishReasonStop
	}
	args := string(bytes.TrimSpace(content))
	var probe struct {
		Kind  string          `json:"kind"`
		Tasks json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(content, &probe); err != nil {
		return nil, finishReasonStop
	}
	var name string
	switch {
	case probe.Kind == "status_report":
		name = toolNameStatusReport
	case probe.Kind == "" && len(probe.Tasks) > 0:
		name = toolNameCreatePlan
	default:
		return nil, finishReasonStop
	}
	return []chatToolCall{{
		ID:   "call_" + uuid.NewString(),
		Type: "function",
		Function: chatToolCallFunction{
			Name: name, Arguments: args,
		},
	}}, finishReasonToolCalls
}
