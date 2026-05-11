package capabilities

import (
	"context"
	"sort"
	"sync"

	"agentd/internal/gateway"
)

type CapabilityAdapter interface {
	Name() string
	ListTools(ctx context.Context) ([]gateway.ToolDefinition, error)
	CallTool(ctx context.Context, name string, args map[string]any) (any, error)
	Close() error
}

type Registry struct {
	mu       sync.RWMutex
	adapters map[string]CapabilityAdapter
}

func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[string]CapabilityAdapter),
	}
}

func (r *Registry) Register(name string, adapter CapabilityAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[name] = adapter
}

func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.adapters, name)
}

func (r *Registry) GetAdapter(name string) (CapabilityAdapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[name]
	return adapter, ok
}

func (r *Registry) GetTools(ctx context.Context) ([]gateway.ToolDefinition, error) {
	tools, _, err := r.GetToolsAndAdapterIndex(ctx)
	return tools, err
}

// GetToolsAndAdapterIndex returns all tools from all adapters along with a map
// from tool name to adapter name. The map is useful for routing tool calls without
// repeated adapter iteration.
func (r *Registry) GetToolsAndAdapterIndex(ctx context.Context) ([]gateway.ToolDefinition, map[string]string, error) {
	r.mu.RLock()
	names := make([]string, 0, len(r.adapters))
	for n := range r.adapters {
		names = append(names, n)
	}
	r.mu.RUnlock()
	sort.Strings(names)

	var allTools []gateway.ToolDefinition
	toolToAdapter := make(map[string]string)

	for _, name := range names {
		r.mu.RLock()
		adapter, exists := r.adapters[name]
		r.mu.RUnlock()
		if !exists || adapter == nil {
			continue
		}
		tools, err := adapter.ListTools(ctx)
		if err != nil {
			return nil, nil, err
		}
		for _, t := range tools {
			if _, exists := toolToAdapter[t.Name]; !exists {
				toolToAdapter[t.Name] = name
			}
		}
		allTools = append(allTools, tools...)
	}

	return allTools, toolToAdapter, nil
}

func (r *Registry) CallTool(ctx context.Context, adapterName, toolName string, args map[string]any) (any, error) {
	r.mu.RLock()
	adapter, ok := r.adapters[adapterName]
	r.mu.RUnlock()

	if !ok {
		return nil, nil
	}

	return adapter.CallTool(ctx, toolName, args)
}

// AdapterForTool returns the registered adapter name that exposes the given tool name.
// If multiple adapters expose the same tool name, the lexicographically smallest adapter name wins.
func (r *Registry) AdapterForTool(ctx context.Context, toolName string) (adapterName string, ok bool) {
	r.mu.RLock()
	names := make([]string, 0, len(r.adapters))
	for n := range r.adapters {
		names = append(names, n)
	}
	r.mu.RUnlock()
	sort.Strings(names)

	for _, name := range names {
		r.mu.RLock()
		adapter, exists := r.adapters[name]
		r.mu.RUnlock()
		if !exists || adapter == nil {
			continue
		}
		tools, err := adapter.ListTools(ctx)
		if err != nil {
			continue
		}
		for _, t := range tools {
			if t.Name == toolName {
				return name, true
			}
		}
	}
	return "", false
}

func (r *Registry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, adapter := range r.adapters {
		adapter.Close()
	}
}
