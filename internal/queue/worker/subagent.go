package worker

import (
	"context"
	"fmt"

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

	toolExec := NewToolExecutor(d.sandbox, d.workspacePath, d.envVars, d.wallTimeout)
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
	def SubagentDefinition, toolExec *ToolExecutor,
) bool {
	requestMessages := enforceSubagentContextBudget(*messages, def.ContextBudget)
	req := gateway.AIRequest{
		Messages:    requestMessages,
		Temperature: temperature,
		Tools:       tools,
		Provider:    provider,
		Model:       model,
		MaxTokens:   maxTokens,
		Role:        gateway.RoleWorker,
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
		if call.Function.Name == toolNameWrite {
			if path := extractWritePath(call.Function.Arguments); path != "" {
				*filesModified = append(*filesModified, path)
			}
		}
		*messages = append(*messages, gateway.PromptMessage{
			Role:       "tool",
			ToolCallID: call.ID,
			Content:    callResult,
		})
	}
	if i == maxIter-1 {
		result.Status = SubagentStatusTimeout
		result.Error = "max iterations reached"
		result.Output = (*messages)[len(*messages)-1].Content
	}
	result.Iterations = i + 1
	return false
}

// enforceSubagentContextBudget keeps the first two messages (system + user) and
// truncates the rest with the same middle-out strategy as ContextManager (see truncateWorkingMessages).
func enforceSubagentContextBudget(messages []gateway.PromptMessage, budget int) []gateway.PromptMessage {
	if budget <= 0 || totalChars(messages) <= budget {
		return messages
	}
	if len(messages) <= 2 {
		return messages
	}
	fixed := append([]gateway.PromptMessage(nil), messages[:2]...)
	remaining := budget - totalChars(fixed)
	if remaining <= 0 {
		return fixed
	}
	return append(fixed, truncateWorkingMessages(messages[2:], remaining)...)
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
