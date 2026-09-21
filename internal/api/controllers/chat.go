// Package controllers wires HTTP handlers to service-layer logic. The chat
// handler exposes an OpenAI-compatible POST /v1/chat/completions endpoint
// that routes the last user message through Frontdesk before planning.
//
// Wire compatibility is asserted by round-tripping our emitted JSON through
// github.com/openai/openai-go/v3 response types in tests; see the related
// chat tests under internal/api.
package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"agentd/internal/api/correlation"
	"agentd/internal/api/httpx"
	"agentd/internal/frontdesk"
	"agentd/internal/gateway"
	"agentd/internal/memory"
	"agentd/internal/models"
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
	corrID := uuid.NewString()
	ctx := correlation.WithID(r.Context(), corrID)
	log := correlation.Logger(ctx)
	log.DebugContext(ctx, "chat intake: request received")
	req, rawMessage, ok := parseChatRequest(w, r, ctx)
	if !ok {
		log.DebugContext(ctx, "chat intake: request rejected", "error", "invalid_request")
		return
	}
	log.DebugContext(ctx, "chat intake: parsed request",
		"flow", selectedFlow(req), "message_count", len(req.Messages),
		"stream", req.Stream, "model", req.Model, "has_tools", len(req.Tools) > 0,
	)
	intent, files, err := frontdesk.PrepareIntent(h.Planner.Stash, rawMessage, convertFiles(req.Files))
	if err != nil {
		log.WarnContext(ctx, "chat intake: intent preparation failed", "error", err)
		httpx.WriteMappedError(w, err)
		return
	}
	var projectID string
	if len(req.ApprovedScopes) == 1 {
		projectID = req.ApprovedScopes[0]
	}
	userID := r.Header.Get("X-Agentd-User")
	intent = h.prependRecalledContext(ctx, intent, projectID, userID)
	log.DebugContext(ctx, "chat intake: routing to planner",
		"approved_scopes", len(req.ApprovedScopes), "project_id", projectID, "has_files", len(files) > 0,
	)
	if req.Stream {
		h.completeStreaming(w, r, req, intent, files, ctx, log)
		return
	}
	content, err := h.Planner.PlanContent(ctx, req.ApprovedScopes, intent, files)
	log.DebugContext(ctx, "chat intake: request completed",
		"result", requestOutcome(err, content), "content_length", len(content),
	)
	if err != nil {
		if errors.Is(err, frontdesk.ErrMultipleApprovedScopes) {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "send one turn per scope using approved_scopes with exactly one entry")
			return
		}
		if isAICoreTimeout(err) {
			log.WarnContext(ctx, "chat intake: LLM timeout", "error", err)
			httpx.WriteJSON(w, http.StatusOK, completion(req.Model, systemTimeoutMessage, nil, finishReasonStop))
			return
		}
		log.WarnContext(ctx, "chat intake: planner failed", "error", err)
		httpx.WriteMappedError(w, err)
		return
	}
	toolCalls, finishReason := buildToolCalls(content, len(req.Tools) > 0)
	httpx.WriteJSON(w, http.StatusOK, completion(req.Model, string(content), toolCalls, finishReason))
}

func parseChatRequest(w http.ResponseWriter, r *http.Request, ctx context.Context) (chatRequest, string, bool) {
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		slog.WarnContext(ctx, "chat intake: failed to read request body", "error", err)
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "could not read request body")
		return chatRequest{}, "", false
	}
	var req chatRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		slog.WarnContext(ctx, "chat intake: invalid JSON body", "error", err)
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid JSON request body")
		return chatRequest{}, "", false
	}
	message := frontdesk.LastUserMessage(req.Messages)
	if message == "" {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "a user message is required")
		return chatRequest{}, "", false
	}
	return req, message, true
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
	ctx context.Context,
	log *slog.Logger,
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.ErrorContext(ctx, "chat intake: streaming not supported")
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

	finishStop := finishReasonStop
	content, err := h.Planner.PlanContent(ctx, req.ApprovedScopes, intent, files)
	outcome := requestOutcome(err, content)
	switch {
	case errors.Is(err, frontdesk.ErrMultipleApprovedScopes):
		writeFrame(chatChunkDelta{Content: "send one turn per scope using approved_scopes with exactly one entry"}, &finishStop)
	case isAICoreTimeout(err):
		writeFrame(chatChunkDelta{Content: systemTimeoutMessage}, &finishStop)
	case err != nil:
		writeFrame(chatChunkDelta{Content: fmt.Sprintf("error: %v", err)}, &finishStop)
	default:
		toolCalls, finish := buildToolCalls(content, len(req.Tools) > 0)
		f := finish
		writeFrame(chatChunkDelta{Content: string(content), ToolCalls: toolCalls}, &f)
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	flusher.Flush()
	log.DebugContext(ctx, "chat intake: streaming request completed",
		"result", outcome, "content_length", len(content),
	)
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

// selectedFlow describes which intake processing path a request will take so
// the intake log can distinguish streaming vs agentic-tool vs plain chat turns
// without logging the full user content.
func selectedFlow(req chatRequest) string {
	if req.Stream {
		return "stream"
	}
	if len(req.Tools) > 0 {
		return "agentic"
	}
	return "chat"
}

// requestOutcome is the intake-level outcome used in the "request completed"
// log. It is computed before any error-handoff so the outcome is always
// recorded for the correlation trace.
func requestOutcome(err error, content []byte) string {
	if err != nil {
		return "error"
	}
	if len(content) == 0 {
		return "empty"
	}
	return "ok"
}
