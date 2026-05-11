package capabilities

import (
	"context"
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
	mu         sync.RWMutex
	adapters   map[string]CapabilityAdapter
	allTools   []gateway.ToolDefinition
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
	r.mu.RLock()
	defer r.mu.RUnlock()

	var allTools []gateway.ToolDefinition
	for _, adapter := range r.adapters {
		tools, err := adapter.ListTools(ctx)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, tools...)
	}

	r.allTools = allTools
	return allTools, nil
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

func (r *Registry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, adapter := range r.adapters {
		adapter.Close()
	}
}
