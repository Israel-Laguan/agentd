package subagent

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"

	agentcontext "agentd/internal/agent/context"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
)

// Delegate runs a subagent with isolated context and restricted tools.
// The subagent's internal reasoning never enters the parent's context.
func (d *SubagentDelegate) Delegate(
	ctx context.Context,
	def SubagentDefinition,
	taskDescription string,
	provider, model string,
	temperature float64,
	maxTokens int,
) (*SubagentResult, error) {
	if d.depth >= d.delegationDepthLimit() {
		return nil, ErrDepthExceeded
	}

	maxIter := def.MaxIterations
	if maxIter <= 0 {
		maxIter = 20
	}

	toolExec := agenttools.NewToolExecutor(d.sandbox, d.workspacePath, d.envVars, d.wallTimeout).WithReadOnlyWorkspace(d.readOnlyWorkspace).WithIntent(d.intent)
	tools := d.buildToolSet(def, toolExec)

	systemPrompt := d.buildSystemPrompt(def)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: taskDescription},
	}

	result := &SubagentResult{
		Status: SubagentStatusSuccess,
	}
	toolsCalled := make(map[string]struct{})
	var filesModified []string

	for i := 0; i < maxIter; i++ {
		done := d.runSubagentStep(ctx, i, maxIter, &messages, tools, provider, model, temperature, maxTokens, result, toolsCalled, &filesModified, def, toolExec)
		if done {
			break
		}
	}

	for tool := range toolsCalled {
		result.ToolsCalled = append(result.ToolsCalled, tool)
	}
	result.FilesModified = filesModified
	return result, nil
}

func (d *SubagentDelegate) runSubagentStep(
	ctx context.Context, i, maxIter int,
	messages *[]gateway.PromptMessage, tools []gateway.ToolDefinition,
	provider, model string, temperature float64, maxTokens int,
	result *SubagentResult, toolsCalled map[string]struct{}, filesModified *[]string,
	def SubagentDefinition, toolExec *agenttools.ToolExecutor,
) bool {
	requestMessages := enforceSubagentContextBudget(*messages, def.ContextBudget, d.truncation)
	req := gateway.AIRequest{
		Purpose:     gateway.PurposeSubagentTurn,
		Messages:    requestMessages,
		Temperature: temperature,
		Tools:       tools,
		Provider:    provider,
		Model:       model,
		MaxTokens:   maxTokens,
		TaskID:      gateway.TaskIDFromContext(ctx),
	}
	resp, err := d.gateway.Generate(ctx, req)
	if err != nil {
		result.Status = SubagentStatusFailure
		result.Error = fmt.Sprintf("gateway error: %v", err)
		result.Iterations = i + 1
		return true
	}
	*messages = append(*messages, gateway.PromptMessage{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: append([]gateway.ToolCall(nil), resp.ToolCalls...),
	})
	if len(resp.ToolCalls) == 0 {
		result.Output = resp.Content
		result.Iterations = i + 1
		return true
	}
	for _, call := range resp.ToolCalls {
		toolsCalled[call.Function.Name] = struct{}{}
		callResult := d.executeTool(ctx, call, def, toolExec)
		if call.Function.Name == agenttools.ToolNameWrite {
			if path := extractWritePath(call.Function.Arguments); path != "" {
				*filesModified = append(*filesModified, path)
			}
		}
		*messages = append(*messages, gateway.PromptMessage{
			Role:       "tool",
			ToolCallID: call.ID,
			Content:    callResult,
		})
		if failSandboxUnavailable(call, callResult, result) {
			result.Iterations = i + 1
			return true
		}
	}
	if i == maxIter-1 {
		result.Status = SubagentStatusTimeout
		result.Error = "max iterations reached"
		result.Output = (*messages)[len(*messages)-1].Content
	}
	result.Iterations = i + 1
	return false
}

// failSandboxUnavailable ends the child when a bash call was refused by an
// unavailable required jail: the model cannot fix a missing jail, so the result
// carries the SANDBOX_UNAVAILABLE code for the parent to fail terminally (B-052).
func failSandboxUnavailable(call gateway.ToolCall, raw string, result *SubagentResult) bool {
	if call.Function.Name != agenttools.ToolNameBash {
		return false
	}
	tr := agenttools.ClassifyBuiltinToolResult(call.ID, call.Function.Name, raw, 0)
	if tr.Error == nil || tr.Error.Code != agenttools.ToolErrorCodeSandboxUnavailable {
		return false
	}
	result.Status = SubagentStatusFailure
	result.Error = tr.Error.Message
	result.ErrorCode = tr.Error.Code
	return true
}

// enforceSubagentContextBudget keeps the first two messages (system + user) and
// truncates the rest with the parent's ContextManager strategy (nil: middle-out).
func enforceSubagentContextBudget(messages []gateway.PromptMessage, budget int, strategy gateway.TruncationStrategy) []gateway.PromptMessage {
	if budget <= 0 || agentcontext.TotalChars(messages) <= budget {
		return messages
	}
	if len(messages) <= 2 {
		return messages
	}
	fixed := append([]gateway.PromptMessage(nil), messages[:2]...)
	remaining := budget - agentcontext.TotalChars(fixed)
	if remaining <= 0 {
		return fixed
	}
	return append(fixed, agentcontext.TruncateWorkingMessages(messages[2:], remaining, strategy)...)
}

// DelegateParallel runs multiple subagent tasks concurrently and collects results.
func (d *SubagentDelegate) DelegateParallel(
	ctx context.Context,
	tasks []ParallelTask,
	provider, model string,
	temperature float64,
	maxTokens int,
) []*SubagentResult {
	const maxConcurrentDelegates = 8
	results := make([]*SubagentResult, len(tasks))
	done := make(chan struct{}, len(tasks))
	sem := make(chan struct{}, maxConcurrentDelegates)

	for i, task := range tasks {
		go func(idx int, t ParallelTask) {
			sem <- struct{}{}
			defer func() {
				<-sem
				done <- struct{}{}
			}()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("parallel subagent panic recovered", "subagent", t.Definition.Name, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
					results[idx] = &SubagentResult{Status: SubagentStatusFailure, Error: fmt.Sprintf("subagent panicked: %v", r)}
				}
			}()
			res, err := d.Delegate(ctx, t.Definition, t.Description, provider, model, temperature, maxTokens)
			if err != nil {
				results[idx] = &SubagentResult{
					Status: SubagentStatusFailure,
					Error:  err.Error(),
				}
			} else {
				results[idx] = res
			}
		}(i, task)
	}

	for range tasks {
		<-done
	}
	return results
}
